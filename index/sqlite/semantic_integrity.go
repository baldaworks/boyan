package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	semantic "github.com/baldaworks/boyan/internal/semantic"
)

// validateSemanticBuild holds at most one chunk's metadata/vector in addition
// to bounded coverage. Only LoadReusable's callback retains vector material.
//
//nolint:cyclop,gocognit,funlen // Streaming integrity gates keep rejection next to the data being certified.
func validateSemanticBuild(
	ctx context.Context,
	tx *sql.Tx,
	build string,
	visit func(semantic.ProjectionChunk),
) (semantic.State, error) {
	state, _, err := readSemanticState(ctx, tx, build)
	if err != nil {
		return semantic.State{}, err
	}

	if state.Status != semantic.StatusReady {
		return state, nil // readSemanticState validates degraded shape and absence of rows.
	}

	coverage, byRevision, err := readSemanticCoverage(ctx, tx, build)
	if err != nil {
		return semantic.State{}, err
	}

	if uint32(len(coverage)) != state.RecordCount || semantic.CoverageFingerprint(coverage) != state.CoverageHash {
		return semantic.State{}, semantic.ErrCorrupt
	}

	if err = checkSemanticChunkBounds(ctx, tx, build); err != nil {
		return semantic.State{}, err
	}

	rows, err := tx.QueryContext(
		ctx,
		`SELECT rowid,item_id,revision_id,revision,ordinal,content_hash,input_hash,change_seq,
  dimensions,embedding,kind,category,memory_key,created_at,observed_at,valid_from,valid_until,expires_at
  FROM semantic_chunk WHERE build_id=? ORDER BY item_id,revision_id,ordinal LIMIT 8193`,
		build,
	)
	if err != nil {
		return semantic.State{}, err
	}

	defer func() { _ = rows.Close() }()

	seen := make(map[string]uint32, len(coverage))

	var (
		first semantic.ProjectionChunk
		count uint32
		bytes uint64
	)

	for rows.Next() {
		if err = ctx.Err(); err != nil {
			return semantic.State{}, err
		}

		if count >= state.ChunkCount {
			return semantic.State{}, semantic.ErrCorrupt
		}

		chunk, readErr := readSemanticChunk(ctx, tx, rows, state)
		if readErr != nil {
			return semantic.State{}, readErr
		}

		manifest, found := byRevision[chunk.RevisionID]
		if !semantic.ChunkMatchesCoverage(chunk, manifest, found, seen[chunk.RevisionID]) ||
			semantic.ValidateChunk(semantic.StateFromGeneration(state), chunk) != nil || chunk.ObservedAt == nil {
			return semantic.State{}, semantic.ErrCorrupt
		}

		if first.RevisionID == chunk.RevisionID {
			if !semantic.SameRecordMetadata(first, chunk) {
				return semantic.State{}, semantic.ErrCorrupt
			}
		} else {
			first = chunk
			first.Vector = nil
		}

		encoded, encodeErr := semantic.EncodeChunk(chunk)
		if encodeErr != nil || len(encoded) > semantic.MaxChunkValueBytes {
			return semantic.State{}, semantic.ErrCorrupt
		}

		bytes += uint64(semantic.LogicalChunkKeyBytes + len(encoded))
		if semantic.CheckProjectionBounds(int(count+1), bytes) != nil {
			return semantic.State{}, semantic.ErrCorrupt
		}

		seen[chunk.RevisionID]++
		count++

		if visit != nil {
			visit(chunk)
		}
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

	finalized, err := semantic.FinalizeEncodedBytes(state, bytes)
	if err != nil || finalized.EncodedBytes != state.EncodedBytes {
		return semantic.State{}, semantic.ErrCorrupt
	}

	return state, nil
}

//nolint:cyclop,funlen // Bounded coverage validation preserves cancellation separately from corruption.
func readSemanticCoverage(
	ctx context.Context,
	tx *sql.Tx,
	build string,
) ([]semantic.CoverageRecord, map[string]semantic.CoverageRecord, error) {
	var invalid bool

	const bounds = `SELECT count(*)>8192 OR coalesce(max(length(CAST(item_id AS BLOB))),0)>512
  OR coalesce(max(length(CAST(revision_id AS BLOB))),0)>512
  OR coalesce(max(length(CAST(content_hash AS BLOB))),0)>64 FROM semantic_coverage WHERE build_id=?`
	if err := tx.QueryRowContext(ctx, bounds, build).Scan(&invalid); err != nil {
		return nil, nil, err
	}

	if invalid {
		return nil, nil, semantic.ErrCorrupt
	}

	rows, err := tx.QueryContext(
		ctx,
		`SELECT item_id,revision_id,revision,content_hash,chunk_count FROM semantic_coverage
		 WHERE build_id=? ORDER BY item_id,revision_id LIMIT 8193`,
		build,
	)
	if err != nil {
		return nil, nil, err
	}

	defer func() { _ = rows.Close() }()

	var coverage []semantic.CoverageRecord

	for rows.Next() {
		var (
			record   semantic.CoverageRecord
			revision []byte
		)
		if err = rows.Scan(
			&record.ItemID,
			&record.RevisionID,
			&revision,
			&record.ContentHash,
			&record.ChunkCount,
		); err != nil {
			return nil, nil, err
		}

		record.Revision, err = decodeUint64(revision)
		if err != nil || semantic.ValidateCoverageRecord(record) != nil {
			return nil, nil, semantic.ErrCorrupt
		}

		coverage = append(coverage, record)
	}

	if err = rows.Err(); err != nil {
		return nil, nil, err
	}

	ordered, byRevision, err := semantic.OrderCoverage(ctx, coverage)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}

		return nil, nil, semantic.ErrCorrupt
	}

	return ordered, byRevision, nil
}

func checkSemanticChunkBounds(ctx context.Context, tx *sql.Tx, build string) error {
	var invalid bool

	const query = `SELECT EXISTS(SELECT 1 FROM semantic_chunk c WHERE c.build_id=? AND (
  length(CAST(c.item_id AS BLOB))>512 OR length(CAST(c.revision_id AS BLOB))>512
  OR length(CAST(c.content_hash AS BLOB))>64 OR length(CAST(c.input_hash AS BLOB))>64
  OR length(CAST(c.kind AS BLOB))>512 OR length(CAST(c.category AS BLOB))>512 OR length(CAST(c.memory_key AS BLOB))>512
  OR c.dimensions NOT BETWEEN 1 AND 4096 OR length(c.embedding)!=4*c.dimensions
  OR (SELECT count(*) FROM semantic_source s WHERE s.chunk_rowid=c.rowid)>256
  OR (SELECT count(*) FROM semantic_conversation s WHERE s.chunk_rowid=c.rowid)>256
  OR EXISTS(SELECT 1 FROM semantic_source s WHERE s.chunk_rowid=c.rowid AND length(CAST(s.id AS BLOB))>512)
  OR EXISTS(SELECT 1 FROM semantic_conversation s WHERE s.chunk_rowid=c.rowid AND length(CAST(s.id AS BLOB))>512)))`
	if err := tx.QueryRowContext(ctx, query, build).Scan(&invalid); err != nil {
		return err
	}

	if invalid {
		return semantic.ErrCorrupt
	}

	return nil
}

//nolint:funlen // Explicit field decoding preserves the complete portable chunk contract.
func readSemanticChunk(
	ctx context.Context,
	tx *sql.Tx,
	rows *sql.Rows,
	state semantic.State,
) (semantic.ProjectionChunk, error) {
	chunk := semantic.ProjectionChunk{
		Scope:               state.Scope,
		BuildID:             state.BuildID,
		LexicalGenerationID: state.LexicalGenerationID,
		LexicalWatermark:    state.LexicalWatermark,
		ModelSpace:          state.ModelSpace,
	}

	var (
		rowid                                                               int64
		revision, sequence, vector, created, observed, from, until, expires []byte
	)

	err := rows.Scan(
		&rowid,
		&chunk.ItemID,
		&chunk.RevisionID,
		&revision,
		&chunk.Ordinal,
		&chunk.ContentHash,
		&chunk.InputHash,
		&sequence,
		&chunk.Dimensions,
		&vector,
		&chunk.Kind,
		&chunk.Category,
		&chunk.MemoryKey,
		&created,
		&observed,
		&from,
		&until,
		&expires,
	)
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

	chunk.Vector, err = decodeSemanticVector(vector, chunk.Dimensions)
	if err != nil {
		return semantic.ProjectionChunk{}, err
	}

	chunk.CreatedAt, err = decodeTime(created)
	if err != nil {
		return semantic.ProjectionChunk{}, semantic.ErrCorrupt
	}

	for _, field := range []struct {
		encoded []byte
		target  **time.Time
	}{{observed, &chunk.ObservedAt}, {from, &chunk.ValidFrom}, {until, &chunk.ValidUntil}, {expires, &chunk.ExpiresAt}} {
		if field.encoded != nil {
			value, decodeErr := decodeTime(field.encoded)
			if decodeErr != nil {
				return semantic.ProjectionChunk{}, semantic.ErrCorrupt
			}

			*field.target = &value
		}
	}

	chunk.SourceIDs, err = readSemanticIDs(
		ctx,
		tx,
		"SELECT id FROM semantic_source WHERE chunk_rowid=? ORDER BY id LIMIT 257",
		rowid,
	)
	if err != nil {
		return semantic.ProjectionChunk{}, err
	}

	chunk.ConversationIDs, err = readSemanticIDs(
		ctx,
		tx,
		"SELECT id FROM semantic_conversation WHERE chunk_rowid=? ORDER BY id LIMIT 257",
		rowid,
	)

	return chunk, err
}

func readSemanticIDs(ctx context.Context, tx *sql.Tx, query string, rowid int64) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query, rowid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var ids []string

	for rows.Next() {
		var value string
		if err = rows.Scan(&value); err != nil {
			return nil, err
		}

		ids = append(ids, value)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	if len(ids) > semantic.MaxMetadataIDs || !semantic.ValidSortedIdentities(ids) {
		return nil, semantic.ErrCorrupt
	}

	return ids, nil
}

// inspectSemantic revokes only demonstrably invalid complete builds. It neither
// publishes nor retires rows, and preserves valid unreachable pending stages.
//
//nolint:cyclop,funlen // Structural failures propagate; demonstrated logical corruption alone revokes completeness.
func inspectSemantic(ctx context.Context, db *sql.DB) error {
	for _, query := range []string{
		`SELECT build_id,physical_build_id,complete,status,epoch FROM semantic_build LIMIT 0`,
		`SELECT scope_kind,scope_key,build_id,epoch FROM semantic_active LIMIT 0`,
		`SELECT build_id,item_id,revision_id,revision,content_hash,chunk_count FROM semantic_coverage LIMIT 0`,
		`SELECT rowid,build_id,item_id,revision_id,revision,ordinal,content_hash,input_hash,change_seq,
		 dimensions,embedding,kind,category,memory_key,created_at,observed_at,valid_from,valid_until,expires_at
		 FROM semantic_chunk LIMIT 0`,
		`SELECT chunk_rowid,id FROM semantic_source LIMIT 0`,
		`SELECT chunk_rowid,id FROM semantic_conversation LIMIT 0`,
	} {
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			return semanticStorageError(err)
		}

		_ = rows.Close()
	}

	var after int64

	for {
		var (
			rowid int64
			build sql.NullString
		)

		err := db.QueryRowContext(ctx, `SELECT rowid,CASE WHEN length(CAST(build_id AS BLOB))<=512 THEN build_id END
   FROM semantic_build WHERE complete=1 AND rowid>?
   ORDER BY rowid LIMIT 1`, after).Scan(&rowid, &build)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}

		if err != nil {
			return err
		}

		after = rowid

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}

		if !build.Valid || !semantic.ValidProjectionIdentity(build.String) {
			err = semantic.ErrCorrupt
		} else {
			_, err = validateSemanticBuild(ctx, tx, build.String, nil)
		}

		if errors.Is(err, semantic.ErrCorrupt) {
			_, err = tx.ExecContext(ctx, "UPDATE semantic_build SET complete=0 WHERE rowid=?", rowid)
		}

		if err != nil {
			_ = tx.Rollback()

			return err
		}

		if err = tx.Commit(); err != nil {
			return err
		}
	}
}
