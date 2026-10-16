package sqlite

import (
	"context"
	"database/sql"
	"time"

	semantic "github.com/baldaworks/boyan/internal/semantic"
)

// validateSearchMetadata retains one chunk and its first sibling's metadata.
// Actual stored components are validated only by SQL. A synthetic unit vector
// lets the portable validator check the other fields without weakening it.
// JSON encodes vector bytes as base64, whose size depends only on dimensions,
// so this placeholder also preserves the exact portable encoded-byte budget.
//
//nolint:cyclop,funlen // Keep complete coverage, metadata and logical-byte integrity gates together.
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

	unit := make([]float32, state.Dimensions)
	unit[0] = 1
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

		chunk, readErr := readSearchChunkMetadata(ctx, snapshot.tx, rows, state)
		if readErr != nil {
			return readErr
		}

		chunk.Vector = unit

		manifest, found := byRevision[chunk.RevisionID]
		if !semantic.ChunkMatchesCoverage(chunk, manifest, found, seen[chunk.RevisionID]) ||
			semantic.ValidateChunk(semantic.StateFromGeneration(state), chunk) != nil || chunk.ObservedAt == nil {
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

		encoded, encodeErr := semantic.EncodeChunk(chunk)
		if encodeErr != nil || len(encoded) > semantic.MaxChunkValueBytes {
			return semantic.ErrCorrupt
		}

		bytes += uint64(semantic.LogicalChunkKeyBytes + len(encoded))

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

func readSearchChunkMetadata(ctx context.Context, tx *sql.Tx, rows *sql.Rows,
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

	chunk.SourceIDs, err = readSemanticIDs(ctx, tx,
		"SELECT id FROM semantic_source WHERE chunk_rowid=? ORDER BY id LIMIT 257", rowid)
	if err != nil {
		return semantic.ProjectionChunk{}, err
	}

	chunk.ConversationIDs, err = readSemanticIDs(ctx, tx,
		"SELECT id FROM semantic_conversation WHERE chunk_rowid=? ORDER BY id LIMIT 257", rowid)

	return chunk, err
}
