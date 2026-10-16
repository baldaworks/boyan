package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/baldaworks/boyan/pkg/memory"
)

func (a *SQLiteCanonicalApplier) ScrubCanonicalForget(ctx context.Context,
	scope memory.Scope, sources, revisions []string) (result error) {
	defer func() { result = lexicalStorageError(result) }()

	if err := validateScrub(ctx, scope, sources, revisions); err != nil {
		return err
	}

	operation, done, err := a.acquire(ctx)
	if err != nil {
		return err
	}

	ctx = operation
	defer func() {
		if result != nil && ctx.Err() != nil {
			result = ctx.Err()
		}

		done()
	}()

	unlock, err := a.lockScope(ctx, scope)
	if err != nil {
		return err
	}
	defer unlock()

	if len(sources)+len(revisions) == 0 {
		return ctx.Err()
	}

	var enabled int

	const probe = `SELECT count(*) FROM sqlite_schema WHERE name='semantic_build'`

	err = a.store.reader.QueryRowContext(ctx, probe).Scan(&enabled)
	if err != nil {
		return err
	}

	for _, input := range []struct {
		ids    []string
		source bool
	}{{sources, true}, {revisions, false}} {
		if err = a.scrubIdentityPages(ctx, scope, input.ids, input.source, enabled != 0); err != nil {
			return err
		}
	}

	return nil
}

//nolint:cyclop // Validate every scope and supplied identity before mutation starts.
func validateScrub(ctx context.Context, scope memory.Scope, sources, revisions []string) error {
	if ctx == nil {
		return errors.New("sqlite scrub context is nil")
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := scope.Validate(); err != nil {
		return err
	}
	// Preserve the existing Bleve forget port's bounded identity-array contract.
	if len(sources) > memory.MaxSnapshotItems || len(revisions) > memory.MaxSnapshotItems {
		return memory.PermanentError(memory.CodeLimitExceeded, "sqlite forget identity bound exceeded", nil)
	}

	for _, ids := range [][]string{sources, revisions} {
		for _, id := range ids {
			if id == "" || strings.TrimSpace(id) != id || strings.ContainsAny(id, "\r\n\t") {
				return memory.PermanentError(memory.CodeInvalidDerived, "sqlite forget identity is invalid", nil)
			}
		}
	}

	return nil
}

//nolint:cyclop // One page dispatches lexical rows, semantic chunks and zero-chunk coverage independently.
func (a *SQLiteCanonicalApplier) scrubIdentityPages(ctx context.Context, scope memory.Scope,
	ids []string, source, semantic bool) error {
	for len(ids) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}

		size := uint64(len(scope.Kind)) + uint64(len(scope.Key))

		end := 0
		for end < len(ids) && end < lexicalPageRecords {
			next, err := checkedInputBytes(size, uint64(len(ids[end])))
			if err != nil {
				return err
			}

			if end > 0 && next > lexicalPageBytes {
				break
			}

			size = next
			end++

			if size > lexicalPageBytes {
				break
			}
		}

		page := ids[:end]

		if semantic {
			// Retain source-to-revision proof until every matching semantic row is
			// removed. A canceled page can retry using the surviving lexical/chunk proof.
			if err := a.scrubZeroCoverage(ctx, scope, page, source); err != nil {
				return err
			}

			predicate, args := scrubPredicate(scope, page, source, true)
			if err := a.scrubRows(ctx, predicate, args, true); err != nil {
				return err
			}
		}

		predicate, args := scrubPredicate(scope, page, source, false)
		if err := a.scrubRows(ctx, predicate, args, false); err != nil {
			return err
		}

		ids = ids[end:]
	}

	return nil
}

// Only placeholder counts and fixed application SQL form these statements.
func scrubPredicate(scope memory.Scope, ids []string, source, semantic bool) (string, []any) {
	predicate := `r.revision_id IN (` + placeholders(len(ids)) + `)`

	if source {
		membership, owner := "lexical_source", "record_rowid"
		if semantic {
			membership, owner = "semantic_source", "chunk_rowid"
		}

		predicate = `EXISTS(SELECT 1 FROM ` + membership + ` s WHERE s.` + owner + `=r.rowid
 AND s.id IN (` + placeholders(len(ids)) + `))`
	}

	args := []any{scope.Kind, scope.Key}
	for _, id := range ids {
		args = append(args, id)
	}

	return `b.scope_kind=? AND b.scope_key=? AND ` + predicate, args
}

func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }

type scrubRow struct {
	id              int64
	bytes           uint64
	owner, revision string
	recheck         string
	args            []any
}

// Lexical byte expressions count UTF-8 bytes, never SQLite character counts.
// They are a conservative input-size signal for isolating large row cascades;
// they do not promise a bound on physical FTS/FK/WAL deletion work.
const lexicalScrubBytes = `length(CAST(r.item_id AS BLOB))+2*length(CAST(r.revision_id AS BLOB))+
 2*length(CAST(r.physical_build_id AS BLOB))+length(r.revision)+length(r.change_seq)+
 length(CAST(r.kind AS BLOB))+length(CAST(r.category AS BLOB))+length(CAST(r.memory_key AS BLOB))+
 length(r.created_at)+length(r.observed_at)+coalesce(length(r.valid_from),0)+
 coalesce(length(r.valid_until),0)+coalesce(length(r.expires_at),0)+
 length(CAST(r.sensitivity AS BLOB))+length(CAST(r.retention AS BLOB))+
 2*(length(CAST(r.raw_tokens AS BLOB))+length(CAST(r.ru_tokens AS BLOB))+length(CAST(r.en_tokens AS BLOB)))+32+
 coalesce((SELECT sum(length(CAST(s.id AS BLOB))+8) FROM lexical_source s WHERE s.record_rowid=r.rowid),0)+
 coalesce((SELECT sum(length(CAST(c.id AS BLOB))+8) FROM lexical_conversation c WHERE c.record_rowid=r.rowid),0)`

//nolint:cyclop // Enumeration and record-sized deletion groups both propagate errors before continuing.
func (a *SQLiteCanonicalApplier) scrubRows(ctx context.Context, predicate string, args []any, semantic bool) error {
	query := `SELECT r.rowid AS id,` + lexicalScrubBytes + ` FROM lexical_record r
 JOIN lexical_build b USING(physical_build_id)
 WHERE ` + predicate + ` ORDER BY r.rowid LIMIT 128`
	if semantic {
		query = `SELECT r.rowid AS id,length(r.embedding)+coalesce((SELECT sum(length(CAST(s.id AS BLOB))+8)
 FROM semantic_source s WHERE s.chunk_rowid=r.rowid),0)+coalesce((SELECT sum(length(CAST(c.id AS BLOB))+8)
	 FROM semantic_conversation c WHERE c.chunk_rowid=r.rowid),0),r.build_id AS owner,r.revision_id AS revision
 FROM semantic_chunk r JOIN semantic_build b USING(build_id) WHERE ` + predicate + ` ORDER BY r.rowid LIMIT 128`
	}

	for {
		rows, err := a.store.scrubCandidates(ctx, query, args)
		if err != nil {
			return err
		}

		if len(rows) == 0 {
			return nil
		}

		for len(rows) > 0 {
			end, size := 0, uint64(0)
			for end < len(rows) {
				next, err := checkedInputBytes(size, rows[end].bytes)
				if err != nil {
					return err
				}

				if end > 0 && next > lexicalPageBytes {
					break
				}

				size = next
				end++

				if size > lexicalPageBytes {
					break
				}
			}

			if err = a.deleteScrubRows(ctx, rows[:end], semantic); err != nil {
				return err
			}

			rows = rows[end:]
		}
	}
}

func (store *Store) scrubCandidates(ctx context.Context, query string, args []any) ([]scrubRow, error) {
	snapshot, err := store.read(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = snapshot.Close() }()

	rows, err := snapshot.tx.QueryContext(snapshot.ctx, query, args...)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var result []scrubRow

	const semanticCandidateColumns = 4

	for rows.Next() {
		var row scrubRow

		destinations := []any{&row.id, &row.bytes}
		if len(columns) == semanticCandidateColumns {
			destinations = append(destinations, &row.owner, &row.revision)
		}

		if err = rows.Scan(destinations...); err != nil {
			return nil, err
		}

		idColumn := "id"
		if columns[0] == "rowid" {
			idColumn = "rowid"
		}

		row.recheck = "SELECT EXISTS(SELECT 1 FROM (" + query + ") WHERE " + idColumn + "=?)"

		row.args = append(append([]any(nil), args...), row.id)
		if len(columns) == semanticCandidateColumns {
			row.recheck = "SELECT EXISTS(SELECT 1 FROM (" + query + ") WHERE id=? AND owner=? AND revision=?)"
			row.args = append(row.args, row.owner, row.revision)
		}

		result = append(result, row)
	}

	return result, rows.Err()
}

func (a *SQLiteCanonicalApplier) deleteScrubRows(ctx context.Context, rows []scrubRow, semantic bool) error {
	return a.store.write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		for _, row := range rows {
			if err := ctx.Err(); err != nil {
				return err
			}

			matches, err := row.matches(ctx, tx)
			if err != nil {
				return err
			}

			if !matches {
				continue
			}

			if semantic {
				if err := scrubSemanticRow(ctx, tx, row.id); err != nil {
					return err
				}
			} else {
				const revoke = `UPDATE lexical_build SET state='building',revoked=1
 WHERE physical_build_id=(SELECT physical_build_id FROM lexical_record WHERE rowid=?)`
				if _, err := tx.ExecContext(ctx, revoke, row.id); err != nil {
					return err
				}

				if err := deleteLexicalRow(ctx, tx, row.id); err != nil {
					return err
				}
			}
		}

		return nil
	})
}

// Candidate identity and the original scope/source/revision predicate are
// checked under writer ownership; SQLite may recycle rowids after retirement.
func (row scrubRow) matches(ctx context.Context, tx *sql.Tx) (bool, error) {
	if row.recheck == "" {
		return false, nil
	}

	var found bool

	err := tx.QueryRowContext(ctx, row.recheck, row.args...).Scan(&found)

	return found, err
}

func scrubSemanticRow(ctx context.Context, tx *sql.Tx, id int64) error {
	var build, revision string

	const lookup = `SELECT build_id,revision_id FROM semantic_chunk WHERE rowid=?`
	if err := tx.QueryRowContext(ctx, lookup, id).Scan(&build, &revision); err == sql.ErrNoRows {
		return nil
	} else if err != nil {
		return err
	}

	if active, err := degradeActiveSemanticBuild(ctx, tx, build); err != nil || active {
		return err
	}

	const revoke = `UPDATE semantic_build SET complete=0,status='degraded',reason='canonical_forget' WHERE build_id=?`
	if _, err := tx.ExecContext(ctx, revoke, build); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM semantic_chunk WHERE rowid=?`, id); err != nil {
		return err
	}

	const coverage = `DELETE FROM semantic_coverage WHERE build_id=? AND revision_id=?
 AND NOT EXISTS(SELECT 1 FROM semantic_chunk WHERE build_id=? AND revision_id=?)`

	_, err := tx.ExecContext(ctx, coverage, build, revision, build, revision)

	return err
}

func (a *SQLiteCanonicalApplier) scrubZeroCoverage(ctx context.Context,
	scope memory.Scope, ids []string, source bool) error {
	query := `SELECT c.rowid AS id,length(CAST(c.revision_id AS BLOB))+length(CAST(c.build_id AS BLOB)),
 c.build_id AS owner,c.revision_id AS revision
 FROM semantic_coverage c JOIN semantic_build b USING(build_id)
 WHERE b.scope_kind=? AND b.scope_key=? AND NOT EXISTS
 (SELECT 1 FROM semantic_chunk r WHERE r.build_id=c.build_id AND r.revision_id=c.revision_id)`
	args := []any{scope.Kind, scope.Key}

	if source {
		// Bind the finite filter once, sharing it across lexical and semantic proof.
		query = `WITH wanted(id) AS (VALUES ` + strings.TrimSuffix(strings.Repeat("(?),", len(ids)), ",") + `) ` + query

		args = nil
		for _, id := range ids {
			args = append(args, id)
		}

		args = append(args, scope.Kind, scope.Key)
		query += ` AND (EXISTS(SELECT 1 FROM lexical_record l
 JOIN lexical_build lb USING(physical_build_id) JOIN lexical_source s ON s.record_rowid=l.rowid
 JOIN wanted w ON w.id=s.id WHERE l.revision_id=c.revision_id
 AND lb.scope_kind=b.scope_kind AND lb.scope_key=b.scope_key)
 OR EXISTS(SELECT 1 FROM semantic_chunk p JOIN semantic_build pb USING(build_id)
 JOIN semantic_source s ON s.chunk_rowid=p.rowid JOIN wanted w ON w.id=s.id
 WHERE p.revision_id=c.revision_id AND pb.scope_kind=b.scope_kind AND pb.scope_key=b.scope_key))`
	} else {
		query += ` AND c.revision_id IN (` + placeholders(len(ids)) + `)`
		for _, id := range ids {
			args = append(args, id)
		}
	}

	query += ` ORDER BY c.rowid LIMIT 128`
	for {
		rows, err := a.store.scrubCandidates(ctx, query, args)
		if err != nil {
			return err
		}

		if len(rows) == 0 {
			return nil
		}

		if err = a.store.write(ctx, func(ctx context.Context, tx *sql.Tx) error {
			return scrubZeroCoverageRows(ctx, tx, rows)
		}); err != nil {
			return err
		}
	}
}

func scrubZeroCoverageRows(ctx context.Context, tx *sql.Tx, rows []scrubRow) error {
	for _, row := range rows {
		matches, err := row.matches(ctx, tx)
		if err != nil {
			return err
		}

		if !matches {
			continue
		}

		if active, err := degradeActiveSemanticBuild(ctx, tx, row.owner); err != nil {
			return err
		} else if active {
			continue
		}

		const revoke = `UPDATE semantic_build SET complete=0,status='degraded',reason='canonical_forget' WHERE build_id=?`
		if _, err = tx.ExecContext(ctx, revoke, row.owner); err != nil {
			return err
		}

		if _, err = tx.ExecContext(ctx, "DELETE FROM semantic_coverage WHERE rowid=?", row.id); err != nil {
			return err
		}
	}

	return nil
}
