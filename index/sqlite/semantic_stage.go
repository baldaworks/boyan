package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"math"

	semantic "github.com/baldaworks/boyan/internal/semantic"
)

// Stage validates portable admission before claiming an unreachable build.
// Provider calls belong to the caller and never run while a SQL transaction is held.
//
//nolint:cyclop,funlen // Admission, ownership and SQL completeness are separate fail-closed gates.
func (store *Store) Stage(ctx context.Context, generation semantic.Generation,
	coverage []semantic.CoverageRecord, chunks []semantic.ProjectionChunk) (semantic.State, error) {
	state, ordered, err := semantic.PrepareProjection(ctx, generation, coverage, chunks)
	if err != nil {
		return semantic.State{}, err
	}

	for _, chunk := range ordered {
		if chunk.ObservedAt == nil {
			return semantic.State{}, semantic.ErrInvalid
		}
	}

	if !store.embeddingsEnabled {
		return semantic.State{}, semantic.ErrInvalid
	}

	operation, done, err := store.semanticScopeLease(ctx, state.Scope)
	if err != nil {
		return semantic.State{}, semanticStorageError(err)
	}
	defer done()

	if _, loaded := store.semanticStaging.LoadOrStore(state.BuildID, semanticOperationStage); loaded {
		return semantic.State{}, semantic.ErrConflict
	}

	defer store.semanticStaging.Delete(state.BuildID)

	orderedCoverage, _, err := semantic.OrderCoverage(operation, coverage)
	if err != nil {
		return semantic.State{}, err
	}

	if err = store.claimSemanticBuild(operation, state); err != nil {
		return semantic.State{}, semanticStorageError(err)
	}

	buffer := semanticStageBuffer{store: store, build: state.BuildID, target: lexicalPageBytes}

	for index := range orderedCoverage {
		record := orderedCoverage[index]
		if err = buffer.add(operation, semanticMutation{coverage: &record}); err != nil {
			return semantic.State{}, semanticStorageError(err)
		}
	}

	if err = buffer.flush(operation); err != nil {
		return semantic.State{}, semanticStorageError(err)
	}

	for index := range ordered {
		if err = buffer.add(operation, semanticMutation{chunk: &ordered[index]}); err != nil {
			return semantic.State{}, semanticStorageError(err)
		}
	}

	if err = buffer.flush(operation); err != nil {
		return semantic.State{}, semanticStorageError(err)
	}
	// Verify the physical rows before granting completeness. No active pointer is changed.
	err = store.write(operation, func(ctx context.Context, tx *sql.Tx) error {
		checked, checkErr := validateSemanticBuild(ctx, tx, state.BuildID, nil)
		if checkErr != nil {
			return checkErr
		}

		if checked != state {
			return semantic.ErrCorrupt
		}

		_, checkErr = tx.ExecContext(
			ctx,
			"UPDATE semantic_build SET complete=1,status='ready' WHERE build_id=? AND complete=0",
			state.BuildID,
		)

		return checkErr
	})
	if err != nil {
		return semantic.State{}, semanticStorageError(err)
	}

	return state, nil
}

func (store *Store) claimSemanticBuild(ctx context.Context, state semantic.State) error {
	return store.write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var exists bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM semantic_build WHERE build_id=?)", state.BuildID).
			Scan(&exists); err != nil {
			return err
		}

		if exists {
			return semantic.ErrConflict
		}

		physical, err := semanticPhysicalSource(ctx, tx, state)
		if err != nil {
			return err
		}

		const insert = `INSERT INTO semantic_build(build_id,scope_kind,scope_key,generation_id,physical_build_id,
   watermark,model_space,dimensions,epoch,status,coverage_hash,revision_count,chunk_count,encoded_bytes,
   omitted_ineligible,omitted_expired,omitted_sensitive,ready_at) VALUES(?,?,?,?,?,?,?,?,?,'building',?,?,?,?,?,?,?,?)`

		_, err = tx.ExecContext(
			ctx,
			insert,
			state.BuildID,
			string(state.Scope.Kind),
			state.Scope.Key,
			state.LexicalGenerationID,
			physical,
			encodeUint64(state.LexicalWatermark),
			state.ModelSpace,
			state.Dimensions,
			encodeUint64(0),
			state.CoverageHash,
			state.RecordCount,
			state.ChunkCount,
			state.EncodedBytes,
			state.Omitted.Ineligible,
			state.Omitted.Expired,
			state.Omitted.Sensitive,
			encodeTime(state.ReadyAt),
		)

		return err
	})
}

type semanticMutation struct {
	coverage *semantic.CoverageRecord
	chunk    *semantic.ProjectionChunk
}

// inputBytes counts SQL bound values, including repeated chunk ownership for
// membership statements. Portable JSON admission is deliberately independent.
func (mutation semanticMutation) inputBytes(build string) (uint64, error) {
	var counter inputByteCounter
	counter.add(len(build))

	if record := mutation.coverage; record != nil {
		counter.add(len(record.ItemID))
		counter.add(len(record.RevisionID))
		counter.add(unsignedBytes)
		counter.add(len(record.ContentHash))
		counter.add(unsignedBytes)
	} else if chunk := mutation.chunk; chunk != nil {
		for _, value := range []string{
			chunk.ItemID, chunk.RevisionID, chunk.ContentHash, chunk.InputHash,
			string(chunk.Kind), string(chunk.Category), string(chunk.MemoryKey),
		} {
			counter.add(len(value))
		}

		// Revision, ordinal, change sequence and dimensions are int64 SQL inputs.
		counter.add(unsignedBytes + unsignedBytes + unsignedBytes + unsignedBytes)
		counter.add(len(chunk.Vector) * semantic.Float32Bytes)
		counter.add(timestampBytes + timestampBytes)

		for _, value := range [][]byte{
			semanticOptionalTime(chunk.ValidFrom), semanticOptionalTime(chunk.ValidUntil),
			semanticOptionalTime(chunk.ExpiresAt),
		} {
			counter.add(len(value))
		}

		for _, values := range [][]string{chunk.SourceIDs, chunk.ConversationIDs} {
			for _, value := range values {
				counter.add(unsignedBytes)
				counter.add(len(value))
			}
		}
	} else {
		return 0, semantic.ErrInvalid
	}

	return counter.bytes, counter.err
}

type semanticStageBuffer struct {
	store     *Store
	build     string
	mutations []semanticMutation
	bytes     uint64
	target    uint64
}

func (buffer *semanticStageBuffer) add(ctx context.Context, mutation semanticMutation) error {
	size, err := mutation.inputBytes(buffer.build)
	if err != nil {
		return err
	}

	if len(buffer.mutations) == lexicalPageRecords || size > buffer.target-buffer.bytes {
		if err = buffer.flush(ctx); err != nil {
			return err
		}
	}

	if size > buffer.target {
		return buffer.store.writeSemanticMutations(ctx, buffer.build, []semanticMutation{mutation})
	}

	buffer.mutations = append(buffer.mutations, mutation)
	buffer.bytes += size

	return nil
}

func (buffer *semanticStageBuffer) flush(ctx context.Context) error {
	if len(buffer.mutations) == 0 {
		return ctx.Err()
	}

	if err := buffer.store.writeSemanticMutations(ctx, buffer.build, buffer.mutations); err != nil {
		return err
	}

	buffer.mutations = nil
	buffer.bytes = 0

	return nil
}

func (store *Store) writeSemanticMutations(ctx context.Context, build string, mutations []semanticMutation) error {
	return store.write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		for _, mutation := range mutations {
			if err := ctx.Err(); err != nil {
				return err
			}

			if record := mutation.coverage; record != nil {
				const insert = `INSERT INTO semantic_coverage(build_id,item_id,revision_id,revision,content_hash,chunk_count)
				 VALUES(?,?,?,?,?,?)`
				if _, err := tx.ExecContext(
					ctx,
					insert,
					build,
					record.ItemID,
					record.RevisionID,
					encodeUint64(record.Revision),
					record.ContentHash,
					record.ChunkCount,
				); err != nil {
					return err
				}
			} else if err := insertSemanticChunk(ctx, tx, *mutation.chunk); err != nil {
				return err
			}
		}

		return nil
	})
}

//nolint:funlen // Chunk fields and owned memberships form one auditable atomic mutation.
func insertSemanticChunk(ctx context.Context, tx *sql.Tx, chunk semantic.ProjectionChunk) error {
	vector := make([]byte, len(chunk.Vector)*semantic.Float32Bytes)
	for index, value := range chunk.Vector {
		binary.LittleEndian.PutUint32(vector[index*semantic.Float32Bytes:], math.Float32bits(value))
	}

	const insert = `INSERT INTO semantic_chunk(build_id,item_id,revision_id,revision,ordinal,content_hash,input_hash,
  change_seq,dimensions,embedding,kind,category,memory_key,created_at,observed_at,valid_from,valid_until,expires_at)
  VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`

	result, err := tx.ExecContext(
		ctx,
		insert,
		chunk.BuildID,
		chunk.ItemID,
		chunk.RevisionID,
		encodeUint64(chunk.Revision),
		chunk.Ordinal,
		chunk.ContentHash,
		chunk.InputHash,
		encodeUint64(chunk.ScopeChangeSeq),
		chunk.Dimensions,
		vector,
		string(chunk.Kind),
		string(
			chunk.Category,
		),
		string(chunk.MemoryKey),
		encodeTime(chunk.CreatedAt),
		semanticOptionalTime(chunk.ObservedAt),
		semanticOptionalTime(
			chunk.ValidFrom,
		),
		semanticOptionalTime(chunk.ValidUntil),
		semanticOptionalTime(chunk.ExpiresAt),
	)
	if err != nil {
		return err
	}

	rowid, err := result.LastInsertId()
	if err != nil {
		return err
	}

	for _, value := range chunk.SourceIDs {
		if _, err = tx.ExecContext(
			ctx,
			"INSERT INTO semantic_source(chunk_rowid,id) VALUES(?,?)",
			rowid,
			value,
		); err != nil {
			return err
		}
	}

	for _, value := range chunk.ConversationIDs {
		if _, err = tx.ExecContext(
			ctx,
			"INSERT INTO semantic_conversation(chunk_rowid,id) VALUES(?,?)",
			rowid,
			value,
		); err != nil {
			return err
		}
	}

	return nil
}

func semanticStorageError(err error) error {
	if err == nil || errors.Is(err, semantic.ErrInvalid) || errors.Is(err, semantic.ErrCapacity) ||
		errors.Is(err, semantic.ErrConflict) ||
		errors.Is(err, semantic.ErrCorrupt) {
		return err
	}

	if errors.Is(err, ErrClosed) {
		return semantic.ErrClosed
	}

	return lexicalStorageError(err)
}
