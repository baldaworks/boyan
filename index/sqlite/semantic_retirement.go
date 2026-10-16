package sqlite

import (
	"context"
	"database/sql"

	semantic "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

// validateSemanticRetirement bounds the exact predecessor's control, coverage
// and chunk keys without loading vectors or requiring a current lexical source.
//
//nolint:cyclop,funlen // Every identity/count/ordinal gate precedes destructive retirement.
func validateSemanticRetirement(ctx context.Context, tx *sql.Tx, scope memory.Scope,
	build string, epoch uint64) (semantic.State, error) {
	state, complete, err := readSemanticState(ctx, tx, build)
	if err != nil {
		return semantic.State{}, err
	}

	if !complete || state.Scope != scope || state.PublicationEpoch != epoch || epoch == 0 {
		return semantic.State{}, semantic.ErrCorrupt
	}

	if state.Status == semantic.StatusDegraded {
		return state, nil
	}

	encoded, err := semantic.EncodeStoredState(semantic.StoredProjectionState{
		SchemaVersion: semantic.ProjectionSchemaVersion, Complete: true, State: state,
	})
	if err != nil {
		return semantic.State{}, semantic.ErrCorrupt
	}

	controlBytes := uint64(semantic.LogicalStateKeyBytes + len(encoded))

	minimum := controlBytes + uint64(state.ChunkCount)*semantic.LogicalChunkKeyBytes
	if state.EncodedBytes < minimum || (state.ChunkCount == 0 && state.EncodedBytes != controlBytes) {
		return semantic.State{}, semantic.ErrCorrupt
	}

	coverage, byRevision, err := readSemanticCoverage(ctx, tx, build)
	if err != nil {
		return semantic.State{}, err
	}

	if uint32(len(coverage)) != state.RecordCount || semantic.CoverageFingerprint(coverage) != state.CoverageHash {
		return semantic.State{}, semantic.ErrCorrupt
	}

	var invalid bool

	const bounds = `SELECT EXISTS(SELECT 1 FROM semantic_chunk WHERE build_id=? AND
 (length(CAST(item_id AS BLOB))>512 OR length(CAST(revision_id AS BLOB))>512
 OR length(CAST(content_hash AS BLOB))>64))`
	if err = tx.QueryRowContext(ctx, bounds, build).Scan(&invalid); err != nil {
		return semantic.State{}, err
	}

	if invalid {
		return semantic.State{}, semantic.ErrCorrupt
	}

	rows, err := tx.QueryContext(ctx, `SELECT item_id,revision_id,revision,ordinal,content_hash
 FROM semantic_chunk WHERE build_id=? ORDER BY revision_id,ordinal LIMIT 8193`, build)
	if err != nil {
		return semantic.State{}, err
	}

	defer func() { _ = rows.Close() }()

	seen := make(map[string]uint32, len(coverage))

	var count uint32

	for rows.Next() {
		if err = ctx.Err(); err != nil {
			return semantic.State{}, err
		}

		var (
			chunk    semantic.ProjectionChunk
			revision []byte
		)
		if err = rows.Scan(&chunk.ItemID, &chunk.RevisionID, &revision, &chunk.Ordinal, &chunk.ContentHash); err != nil {
			return semantic.State{}, err
		}

		chunk.Revision, err = decodeUint64(revision)
		if err != nil || count >= state.ChunkCount {
			return semantic.State{}, semantic.ErrCorrupt
		}

		manifest, found := byRevision[chunk.RevisionID]
		if !semantic.ChunkMatchesCoverage(chunk, manifest, found, seen[chunk.RevisionID]) {
			return semantic.State{}, semantic.ErrCorrupt
		}

		seen[chunk.RevisionID]++
		count++
	}

	if err = rows.Err(); err != nil {
		return semantic.State{}, err
	}

	if count != state.ChunkCount {
		return semantic.State{}, semantic.ErrCorrupt
	}

	for _, record := range coverage {
		if seen[record.RevisionID] != record.ChunkCount {
			return semantic.State{}, semantic.ErrCorrupt
		}
	}

	return state, nil
}
