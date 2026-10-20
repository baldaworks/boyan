package canonicalsql

import (
	"context"
	"strconv"
	"strings"
)

func addPredicate(where, condition string) string {
	if where == "" {
		return " WHERE " + condition
	}

	return where + " AND " + condition
}

func pgPartitionPredicate(request Range, def familyDef, where string, args []any) (string, []any) {
	var values [][]byte
	if request.Scope != nil {
		values = [][]byte{[]byte(request.Scope.Key), []byte(request.Scope.Kind)}
	}

	values = append(values, request.Prefix...)
	column := ""

	switch {
	case len(request.Prefix) == len(def.parts) && (!def.scoped || request.Scope != nil):
		column = "logical_key"

		if len(values) == 0 {
			values = [][]byte{{}}
		}
	case len(request.Prefix) == 1 && len(def.parts) == 2:
		column = "prefix_partition"
	case request.Scope != nil:
		column = "scope_partition"
	}

	if column != "" {
		args = append(args, stampKey(values))
		where = addPredicate(where, column+"=$"+strconv.Itoa(len(args)))
	}

	return where, args
}

func qualify(columns []string, alias string) string {
	result := make([]string, len(columns))
	for i, col := range columns {
		result[i] = alias + "." + col
	}

	return strings.Join(result, ",")
}

func pgPageStatement(table string, f Family, def familyDef, where string, order []string, limitParameter int) string {
	keys := append([]string{"row_id"}, def.columns()...)
	// Secondary revision order also needs the bounded numeric column in the key page.
	if f == Revision && strings.Contains(strings.Join(order, ","), "revision_u64") {
		keys = append(keys, "revision_u64")
	}

	selected := append([]string{"row_id"}, def.columns()...)
	selected = append(selected, derivedColumns(f, def)...)

	value := "CASE WHEN length(r.envelope)<=4194304 THEN r.envelope ELSE NULL END,length(r.envelope)"
	if extra := extraColumns(f); len(extra) != 0 {
		value += "," + qualify(extra, "r")
	}

	return "WITH key_page AS MATERIALIZED (SELECT " + strings.Join(keys, ",") + " FROM " + table + where +
		" ORDER BY " + strings.Join(order, ",") + " LIMIT $" + strconv.Itoa(limitParameter) + ") SELECT " +
		qualify(selected, "r") + "," + value + " FROM key_page p JOIN " + table +
		" r ON r.row_id=p.row_id ORDER BY " + qualify(order, "r")
}

func (tx *Tx) pgPage(
	ctx context.Context, f Family, def familyDef, where string, args []any, order []string, limit int,
) ([]Row, error) {
	statement := pgPageStatement(tx.connection.table(def.table), f, def, where, order, len(args)+1)
	args = append(args, limit)

	r, err := tx.q.query(ctx, statement, args...)
	if err != nil {
		return nil, tx.fail(databaseError(ctx, "scan", err))
	}
	defer r.Close()

	var result []Row

	for r.Next() {
		row, err := scanPhysical(r, f, def)
		if err != nil {
			return nil, tx.fail(rowError(ctx, err))
		}

		result = append(result, row.Row)
	}

	if err := r.Err(); err != nil {
		return nil, tx.fail(databaseError(ctx, "scan", err))
	}

	if err := r.Close(); err != nil {
		return nil, tx.fail(databaseError(ctx, "close page", err))
	}

	if err := tx.checkPageIdentities(ctx, f, def, result); err != nil {
		return nil, err
	}

	return result, nil
}
