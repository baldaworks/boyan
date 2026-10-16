package sqlite

import (
	"context"
	"encoding/binary"
	"math"
	"strings"
	"time"

	semantic "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

const semanticEligible = `eligible AS MATERIALIZED (
 SELECT c.revision_id,c.ordinal,c.embedding FROM semantic_chunk c
 JOIN semantic_build b ON b.build_id=c.build_id
 WHERE c.build_id=? AND b.complete=1 AND b.status='ready'
 AND b.scope_kind=? AND b.scope_key=? AND b.model_space=? AND b.dimensions=?
 AND (?='' OR c.kind=?) AND (?='' OR c.category=?) AND (?='' OR c.memory_key=?)
 AND (?='' OR EXISTS(SELECT 1 FROM semantic_source s WHERE s.chunk_rowid=c.rowid AND s.id=?))
 AND (?='' OR EXISTS(SELECT 1 FROM semantic_conversation s WHERE s.chunk_rowid=c.rowid AND s.id=?))
 AND c.observed_at<=? AND (c.valid_from IS NULL OR c.valid_from<=?)
 AND (c.valid_until IS NULL OR c.valid_until>?) AND (c.expires_at IS NULL OR c.expires_at>?)
)`

func semanticEligibleArgs(state semantic.State, request memory.RecallRequest, at time.Time) []any {
	kind, category := "", ""
	if request.Kind != nil {
		kind = string(*request.Kind)
	}

	if request.Category != nil {
		category = string(*request.Category)
	}

	encoded := encodeTime(at)

	return []any{state.BuildID, string(state.Scope.Kind), state.Scope.Key, state.ModelSpace, state.Dimensions,
		kind, kind, category, category, request.MemoryKey, request.MemoryKey, request.SourceID, request.SourceID,
		request.ConversationID, request.ConversationID, encoded, encoded, encoded, encoded}
}

func semanticRankQuery(windows int) string {
	return `WITH query_windows(q) AS (VALUES ` + strings.TrimSuffix(strings.Repeat("(?),", windows), ",") + `), ` +
		semanticEligible + `, scored AS MATERIALIZED (
 SELECT revision_id,ordinal,max(-1.0,min(1.0,1.0-vec_distance_cosine(embedding,q))) AS score
 FROM eligible CROSS JOIN query_windows
), maxima AS (SELECT revision_id,max(score) AS score FROM scored GROUP BY revision_id)
 SELECT m.revision_id,min(s.ordinal),m.score FROM maxima m JOIN scored s
 ON s.revision_id=m.revision_id AND s.score=m.score GROUP BY m.revision_id,m.score
 ORDER BY m.score DESC,m.revision_id COLLATE BINARY ASC LIMIT ?`
}

func encodeQueryVector(vector []float32) []byte {
	encoded := make([]byte, len(vector)*semantic.Float32Bytes)
	for index, value := range vector {
		binary.LittleEndian.PutUint32(encoded[index*semantic.Float32Bytes:], math.Float32bits(value))
	}

	return encoded
}

// SearchSemantic ranks every eligible chunk/window in SQL before revision top-k.
//
//nolint:cyclop,funlen // Preserve independent input, compatibility, bound and SQL failure gates.
func (snapshot *sqlSearchSnapshot) SearchSemantic(ctx context.Context, request memory.RecallRequest,
	queries [][]float32, limit int) (hits []semantic.DenseMatch, counts semantic.ScanCounts, result error) {
	operation, done, err := snapshot.requestContext(ctx)
	if err != nil {
		return nil, counts, err
	}
	defer done()
	defer func() {
		if result != nil && operation.Err() != nil {
			result = operation.Err()
		}

		result = semanticStorageError(result)
	}()

	normalized, effective, err := snapshot.normalize(request)
	if err != nil {
		return nil, counts, err
	}

	if limit < 1 || limit > semantic.SemanticCandidateLimit ||
		len(queries) < 1 || len(queries) > semantic.QueryWindowLimit {
		return nil, counts, semantic.ErrInvalid
	}

	if snapshot.stateErr != nil {
		return nil, counts, snapshot.stateErr
	}

	if !snapshot.found || snapshot.state.Status != semantic.StatusReady {
		return nil, counts, semantic.ErrConflict
	}

	var args []any

	for _, query := range queries {
		if semantic.ValidateVector(query, snapshot.state.Dimensions) != nil {
			return nil, counts, semantic.ErrInvalid
		}

		args = append(args, encodeQueryVector(query))
	}

	if err = snapshot.validateSearchBuild(operation); err != nil {
		return nil, counts, err
	}

	eligibleArgs := semanticEligibleArgs(snapshot.state, normalized, effective)
	if err = snapshot.tx.QueryRowContext(operation,
		"WITH "+semanticEligible+" SELECT count(*) FROM eligible", eligibleArgs...).Scan(&counts.ScannedChunks); err != nil {
		return nil, counts, err
	}

	if counts.ScannedChunks > semantic.ProjectionChunkLimit {
		return nil, counts, semantic.ErrCapacity
	}

	args = append(args, eligibleArgs...)
	args = append(args, limit)

	rows, err := snapshot.tx.QueryContext(operation, semanticRankQuery(len(queries)), args...)
	if err != nil {
		return nil, counts, err
	}
	defer rows.Close()

	for rows.Next() {
		var hit semantic.DenseMatch
		if err = rows.Scan(&hit.RevisionID, &hit.ChunkOrdinal, &hit.Score); err != nil {
			return nil, counts, err
		}

		if !semantic.ValidProjectionIdentity(hit.RevisionID) || hit.ChunkOrdinal < 0 ||
			math.IsNaN(hit.Score) || math.IsInf(hit.Score, 0) {
			return nil, counts, semantic.ErrCorrupt
		}

		hits = append(hits, hit)
	}

	return hits, counts, rows.Err()
}

// validateSearchBuild never returns stored vector material to Go. Length/count
// gates run before the selected bounded build reaches any sqlite-vec scalar.
func (snapshot *sqlSearchSnapshot) validateSearchBuild(ctx context.Context) error {
	state := snapshot.state

	var count int
	if err := snapshot.tx.QueryRowContext(ctx,
		"SELECT count(*) FROM semantic_chunk WHERE build_id=?", state.BuildID).Scan(&count); err != nil {
		return err
	}

	if count > semantic.ProjectionChunkLimit {
		return semantic.ErrCapacity
	}

	if uint32(count) != state.ChunkCount {
		return semantic.ErrCorrupt
	}

	if err := checkSemanticChunkBounds(ctx, snapshot.tx, state.BuildID); err != nil {
		return err
	}

	var invalid bool

	const shape = `SELECT EXISTS(SELECT 1 FROM semantic_chunk WHERE build_id=? AND
 (typeof(embedding)!='blob' OR dimensions!=? OR observed_at IS NULL
 OR (valid_from IS NOT NULL AND valid_until IS NOT NULL AND valid_until<valid_from)))`
	if err := snapshot.tx.QueryRowContext(ctx, shape, state.BuildID, state.Dimensions).Scan(&invalid); err != nil {
		return err
	}

	if invalid {
		return semantic.ErrCorrupt
	}

	if err := snapshot.validateSearchMetadata(ctx); err != nil {
		return err
	}

	const vectors = `WITH selected AS MATERIALIZED (SELECT embedding FROM semantic_chunk WHERE build_id=?),
 norms AS MATERIALIZED (SELECT vec_type(embedding) AS kind,vec_length(embedding) AS dimensions,
 vec_distance_L2(embedding,?) AS norm FROM selected)
 SELECT EXISTS(SELECT 1 FROM norms WHERE kind!='float32' OR dimensions!=? OR norm IS NULL
 OR norm>1.7976931348623157e308 OR norm<0 OR abs(norm*norm-1.0)>0.001)`
	if err := snapshot.tx.QueryRowContext(ctx, vectors,
		state.BuildID, make([]byte, state.Dimensions*semantic.Float32Bytes), state.Dimensions).Scan(&invalid); err != nil {
		return err
	}

	if invalid {
		return semantic.ErrCorrupt
	}

	return nil
}
