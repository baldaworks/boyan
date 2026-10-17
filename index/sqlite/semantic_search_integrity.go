package sqlite

import (
	"context"
	"database/sql"
	"time"

	semantic "github.com/baldaworks/boyan/internal/semantic"
)

// validateSearchMetadata retains one chunk and its first sibling's metadata.
// Actual stored components are validated only by SQL. Portable metadata
// validation and exact encoded-size accounting need no vector placeholder.
//
//nolint:cyclop,gocognit,funlen // Keep coverage, streamed membership and byte integrity gates together.
func (snapshot *sqlSearchSnapshot) validateSearchMetadata(ctx context.Context) error {
	state := snapshot.state

	coverage, byRevision, err := readSemanticCoverage(ctx, snapshot.tx, state.BuildID)
	if err != nil {
		return err
	}

	if uint32(len(coverage)) != state.RecordCount || semantic.CoverageFingerprint(coverage) != state.CoverageHash {
		return semantic.ErrCorrupt
	}

	const query = `SELECT rowid,item_id,revision_id,revision,ordinal,content_hash,input_hash,change_seq,
 dimensions,kind,category,memory_key,created_at,observed_at,valid_from,valid_until,expires_at
 FROM semantic_chunk WHERE build_id=? ORDER BY revision_id,ordinal LIMIT 8193`

	rows, err := snapshot.tx.QueryContext(ctx, query, state.BuildID)
	if err != nil {
		return err
	}
	defer rows.Close()

	sources, err := openSearchMemberships(ctx, snapshot.tx, state.BuildID, true)
	if err != nil {
		return err
	}
	defer sources.rows.Close()

	conversations, err := openSearchMemberships(ctx, snapshot.tx, state.BuildID, false)
	if err != nil {
		return err
	}
	defer conversations.rows.Close()

	seen := make(map[string]uint32, len(coverage))

	var (
		first semantic.ProjectionChunk
		count uint32
		bytes uint64
	)

	for rows.Next() {
		if err = ctx.Err(); err != nil {
			return err
		}

		chunk, readErr := readSearchChunkMetadata(rows, state)
		if readErr != nil {
			return readErr
		}

		chunk.SourceIDs, err = sources.read(chunk.RevisionID, chunk.Ordinal)
		if err != nil {
			return err
		}

		chunk.ConversationIDs, err = conversations.read(chunk.RevisionID, chunk.Ordinal)
		if err != nil {
			return err
		}

		manifest, found := byRevision[chunk.RevisionID]
		if !semantic.ChunkMatchesCoverage(chunk, manifest, found, seen[chunk.RevisionID]) ||
			semantic.ValidateChunkMetadata(semantic.StateFromGeneration(state), chunk) != nil || chunk.ObservedAt == nil {
			return semantic.ErrCorrupt
		}

		if first.RevisionID == chunk.RevisionID {
			if !semantic.SameRecordMetadata(first, chunk) {
				return semantic.ErrCorrupt
			}
		} else {
			first = chunk
			first.Vector = nil
		}

		encoded, encodeErr := semantic.EncodedChunkBytes(chunk)
		if encodeErr != nil || encoded > semantic.MaxChunkValueBytes {
			return semantic.ErrCorrupt
		}

		bytes += uint64(semantic.LogicalChunkKeyBytes + encoded)

		count++
		if semantic.CheckProjectionBounds(int(count), bytes) != nil {
			return semantic.ErrCorrupt
		}

		seen[chunk.RevisionID]++
	}

	if err = rows.Err(); err != nil {
		return err
	}

	if count != state.ChunkCount {
		return semantic.ErrCorrupt
	}

	if sources.ready || conversations.ready {
		return semantic.ErrCorrupt
	}

	for _, record := range coverage {
		if seen[record.RevisionID] != record.ChunkCount {
			return semantic.ErrCorrupt
		}
	}

	finalized, err := semantic.FinalizeEncodedBytes(state, bytes)
	if err != nil || finalized.EncodedBytes != state.EncodedBytes {
		return semantic.ErrCorrupt
	}

	return nil
}

// readSearchChunkMetadata deliberately has no embedding SELECT/Scan target.
// Bounds are checked before this reader materializes identities or memberships.
//

func readSearchChunkMetadata(rows *sql.Rows,
	state semantic.State) (semantic.ProjectionChunk, error) {
	chunk := semantic.ProjectionChunk{
		Scope: state.Scope, BuildID: state.BuildID, LexicalGenerationID: state.LexicalGenerationID,
		LexicalWatermark: state.LexicalWatermark, ModelSpace: state.ModelSpace,
	}

	var (
		rowid                                                       int64
		revision, sequence, created, observed, from, until, expires []byte
	)

	err := rows.Scan(&rowid, &chunk.ItemID, &chunk.RevisionID, &revision, &chunk.Ordinal, &chunk.ContentHash,
		&chunk.InputHash, &sequence, &chunk.Dimensions, &chunk.Kind, &chunk.Category, &chunk.MemoryKey,
		&created, &observed, &from, &until, &expires)
	if err != nil {
		return semantic.ProjectionChunk{}, err
	}

	chunk.Revision, err = decodeUint64(revision)
	if err != nil {
		return semantic.ProjectionChunk{}, semantic.ErrCorrupt
	}

	chunk.ScopeChangeSeq, err = decodeUint64(sequence)
	if err != nil {
		return semantic.ProjectionChunk{}, semantic.ErrCorrupt
	}

	chunk.CreatedAt, err = decodeTime(created)
	if err != nil {
		return semantic.ProjectionChunk{}, semantic.ErrCorrupt
	}

	for _, field := range []struct {
		encoded []byte
		target  **time.Time
	}{
		{observed, &chunk.ObservedAt}, {from, &chunk.ValidFrom}, {until, &chunk.ValidUntil}, {expires, &chunk.ExpiresAt},
	} {
		if field.encoded != nil {
			value, decodeErr := decodeTime(field.encoded)
			if decodeErr != nil {
				return semantic.ProjectionChunk{}, semantic.ErrCorrupt
			}

			*field.target = &value
		}
	}

	return chunk, nil
}
