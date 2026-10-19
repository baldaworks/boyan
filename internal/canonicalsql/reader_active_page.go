package canonicalsql

import (
	"context"
	"strconv"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Public active cursors follow TupleKey's byte-length prefix, unlike raw Scan.
// Select in that order before LIMIT; sorting a raw-limited page loses heads.
func (tx *Tx) activeHeadPage(ctx context.Context, request memory.ActiveHeadScanRequest) ([]Row, error) {
	if err := tx.check(ctx, false); err != nil {
		return nil, err
	}

	statement, args, err := tx.activeHeadSelection(request)
	if err != nil {
		return nil, tx.fail(err)
	}

	r, err := tx.q.query(ctx, statement, args...)
	if err != nil {
		return nil, tx.fail(databaseError(ctx, "scan active heads", err))
	}
	defer r.Close()

	def, _ := familyDefinition(Head)

	var page []Row

	for r.Next() {
		row, err := tx.activeHeadRow(r, def)
		if err != nil {
			return nil, tx.fail(rowError(ctx, err))
		}

		page = append(page, row)
	}

	if err := r.Err(); err != nil {
		return nil, tx.fail(databaseError(ctx, "scan active heads", err))
	}

	if err := r.Close(); err != nil {
		return nil, tx.fail(databaseError(ctx, "close active heads", err))
	}

	if tx.connection.pg != nil {
		if err := tx.checkActiveHeadIdentities(ctx, page); err != nil {
			return nil, err
		}
	}

	return page, nil
}

func (tx *Tx) activeHeadSelection(request memory.ActiveHeadScanRequest) (string, []any, error) {
	def, _ := familyDefinition(Head)
	scan := Range{Family: Head, Scope: &request.Scope, Limit: int(request.Limit)}

	where, args, err := rangePredicate(scan, def)
	if err != nil {
		return "", nil, err
	}

	if tx.connection.pg != nil {
		where, args = pgPartitionPredicate(scan, def, where, args)
	}

	if request.AfterItemID != "" {
		if err := validatePart("id", []byte(request.AfterItemID)); err != nil {
			return "", nil, err
		}

		n := len(args) + 1
		where += " AND (length(id)>$" + strconv.Itoa(n) + " OR (length(id)=$" + strconv.Itoa(n) +
			" AND id>$" + strconv.Itoa(n+1) + "))"

		args = append(args, len(request.AfterItemID), []byte(request.AfterItemID))
	}

	statement := tx.activeHeadStatement(where, len(args)+1)
	args = append(args, int(request.Limit))

	return statement, args, nil
}

func (tx *Tx) activeHeadStatement(where string, limit int) string {
	table := tx.connection.table("head")

	bound := " LIMIT $" + strconv.Itoa(limit)
	if tx.connection.pg == nil {
		return "SELECT scope_key,scope_kind,id," + valueSelection(Head) + " FROM " + table + where +
			" ORDER BY length(id),id" + bound
	}

	// Materialize only keys and physical identities; at most Limit envelopes
	// cross the join. Bytea length/comparison uses the original bytes.
	return "WITH active_head_page AS MATERIALIZED (SELECT row_id,id FROM " + table + where +
		" ORDER BY length(id),id" + bound + ") SELECT r.row_id,r.scope_key,r.scope_kind,r.id," +
		"r.logical_key,r.scope_partition,CASE WHEN length(r.envelope)<=4194304 THEN r.envelope ELSE NULL END," +
		"length(r.envelope) FROM active_head_page p JOIN " + table +
		" r ON r.row_id=p.row_id ORDER BY length(p.id),p.id"
}

func (tx *Tx) activeHeadRow(r rows, def familyDef) (Row, error) {
	if tx.connection.pg != nil {
		row, err := scanPhysical(r, Head, def)

		return row.Row, err
	}

	return scanRow(r, Head, def)
}
