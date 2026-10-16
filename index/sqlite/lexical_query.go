package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/baldaworks/boyan/pkg/memory"
)

func (snapshot *readSnapshot) activeLexicalBuild(scope memory.Scope) (string, error) {
	var build string

	const query = `SELECT b.physical_build_id FROM lexical_active a JOIN lexical_build b
 ON b.physical_build_id=a.physical_build_id AND b.scope_kind=a.scope_kind AND b.scope_key=a.scope_key
 WHERE a.scope_kind=? AND a.scope_key=? AND b.state='committed'`

	err := snapshot.tx.QueryRowContext(snapshot.ctx, query, scope.Kind, scope.Key).Scan(&build)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}

	return build, err
}

const lexicalQuery = `SELECT r.item_id,r.revision_id,r.revision,r.change_seq,bm25(lexical_fts,1.0,1.0,1.0)
 FROM lexical_fts JOIN lexical_record r ON r.rowid=lexical_fts.rowid
 JOIN lexical_build b ON b.physical_build_id=r.physical_build_id
 WHERE lexical_fts MATCH ? AND b.physical_build_id=? AND b.scope_kind=? AND b.scope_key=? AND b.state='committed'
 AND (?='' OR r.kind=?) AND (?='' OR r.category=?) AND (?='' OR r.memory_key=?)
 AND (?='' OR EXISTS(SELECT 1 FROM lexical_source s WHERE s.record_rowid=r.rowid AND s.id=?))
 AND (?='' OR EXISTS(SELECT 1 FROM lexical_conversation c WHERE c.record_rowid=r.rowid AND c.id=?))
 AND r.observed_at<=? AND (r.valid_from IS NULL OR r.valid_from<=?)
 AND (r.valid_until IS NULL OR r.valid_until>?) AND (r.expires_at IS NULL OR r.expires_at>?)
 ORDER BY bm25(lexical_fts,1.0,1.0,1.0) ASC,r.revision_id COLLATE BINARY ASC LIMIT ?`

// searchLexicalChannel bounds the future shared hybrid view's lexical channel.
// It deliberately makes no certification claim about the selected build.
func (snapshot *readSnapshot) searchLexicalChannel(request memory.RecallRequest,
	effective time.Time, build string, depth int) ([]memory.RecallProjectionHit, error) {
	if depth < 1 || depth > 100 {
		return nil, memory.PermanentError(memory.CodeInvalidQuery, "invalid recall channel depth", nil)
	}

	return snapshot.searchLexical(request, effective, build, depth)
}

// searchLexical queries a selected physical build inside the caller's SQL view.
// Selection alone is not canonical/physical certification. Future shared
// snapshots can reuse it after certification, without opening another SQL view.
//
//nolint:cyclop // Keep bounded query, optional filters and SQL failure handling in the owning snapshot.
func (snapshot *readSnapshot) searchLexical(request memory.RecallRequest,
	effective time.Time, build string, depth int) ([]memory.RecallProjectionHit, error) {
	if depth < 1 || depth > memory.MaxRecallCandidates {
		return nil, memory.PermanentError(memory.CodeInvalidQuery, "invalid recall candidate depth", nil)
	}

	if err := snapshot.ctx.Err(); err != nil {
		return nil, err
	}

	match := lexicalMatch(request.Query)
	if build == "" || match == "" {
		return nil, nil
	}

	kind, category := "", ""
	if request.Kind != nil {
		kind = string(*request.Kind)
	}

	if request.Category != nil {
		category = string(*request.Category)
	}

	at := encodeTime(effective)

	rows, err := snapshot.tx.QueryContext(snapshot.ctx, lexicalQuery, match, build, request.Scope.Kind, request.Scope.Key,
		kind, kind, category, category, request.MemoryKey, request.MemoryKey,
		request.SourceID, request.SourceID, request.ConversationID, request.ConversationID, at, at, at, at, depth)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hits []memory.RecallProjectionHit

	for rows.Next() {
		hit, err := scanLexicalHit(rows, request.Scope)
		if err != nil {
			return nil, err
		}

		hits = append(hits, hit)
	}

	return hits, rows.Err()
}

func scanLexicalHit(rows *sql.Rows, scope memory.Scope) (memory.RecallProjectionHit, error) {
	var (
		hit                memory.RecallProjectionHit
		revision, sequence []byte
		rank               float64
	)
	if err := rows.Scan(&hit.ItemID, &hit.RevisionID, &revision, &sequence, &rank); err != nil {
		return hit, err
	}

	var err error

	hit.Revision, err = decodeUint64(revision)
	if err != nil {
		return hit, err
	}

	hit.ScopeChangeSeq, err = decodeUint64(sequence)
	if err != nil {
		return hit, err
	}

	if math.IsNaN(rank) || math.IsInf(rank, 0) {
		return hit, fmt.Errorf("%w: lexical rank", ErrCorrupt)
	}

	hit.Scope = scope
	hit.Score = math.Max(0, -rank)
	hit.Lexical = hit.Score

	return hit, nil
}
