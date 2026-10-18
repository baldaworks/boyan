package canonicalsql

import (
	"bytes"
	"context"
	"strconv"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

const compoundParts = 2

// Frames are identity/equality values only. Raw component columns define order.
func derivedColumns(f Family, def familyDef) []string {
	cols := []string{"logical_key"}
	if def.scoped {
		cols = append(cols, "scope_partition")
	}

	if len(def.parts) == compoundParts {
		cols = append(cols, "prefix_partition")
	}

	if f == Message {
		cols = append(cols, "source_partition")
	}

	if f == Revision {
		cols = append(cols, "item_partition")
	}

	return cols
}

func derivedValues(f Family, def familyDef, values, extra [][]byte) [][]byte {
	result := [][]byte{stampKey(values)}
	if def.scoped {
		result = append(result, stampKey(values[:2]))
	}

	if len(def.parts) == compoundParts {
		result = append(result, stampKey(values[:len(values)-1]))
	}

	if f == Message || f == Revision {
		result = append(result, stampKey([][]byte{values[0], values[1], extra[0]}))
	}

	return result
}

func corruptRow() error {
	return memory.PermanentError(memory.CodeStoreFailure, "canonical SQL stored identity or binding is corrupt", nil)
}

type physicalRow struct {
	Row

	id int64
}

func keyFromValues(f Family, def familyDef, values [][]byte) (Key, error) {
	key := Key{Family: f}
	if def.scoped {
		key.Scope = memory.Scope{Key: string(values[0]), Kind: memory.ScopeKind(values[1])}
		key.Parts = values[2:]
	} else if len(def.parts) != 0 {
		key.Parts = values
	} else if len(values[0]) != 0 {
		return Key{}, corruptRow()
	}

	_, _, err := key.values()
	if err != nil {
		return Key{}, corruptRow()
	}

	return key, nil
}

func pgSelection(f Family, def familyDef) string {
	cols := append([]string{"row_id"}, def.columns()...)
	cols = append(cols, derivedColumns(f, def)...)

	return strings.Join(cols, ",") + "," + valueSelection(f)
}

func scanPhysical(r rows, f Family, def familyDef) (physicalRow, error) {
	var result physicalRow

	values := make([][]byte, len(def.columns()))
	derived := make([][]byte, len(derivedColumns(f, def)))
	extra := make([][]byte, len(extraColumns(f)))

	var (
		encoded []byte
		size    int
	)

	dest := []any{&result.id}
	for i := range values {
		dest = append(dest, &values[i])
	}

	for i := range derived {
		dest = append(dest, &derived[i])
	}

	dest = append(dest, &encoded, &size)
	for i := range extra {
		dest = append(dest, &extra[i])
	}

	if err := r.Scan(dest...); err != nil {
		return result, err
	}

	key, err := keyFromValues(f, def, values)
	if err != nil {
		return result, err
	}

	for i, expected := range derivedValues(f, def, values, extra) {
		if !bytes.Equal(expected, derived[i]) {
			return result, corruptRow()
		}
	}

	encoded, err = validateStoredValue(key, encoded, size, extra)
	if err != nil {
		if size > MaxEnvelopeBytes {
			return result, limitError()
		}

		return result, corruptRow()
	}

	result.Row = Row{Key: key, Envelope: encoded}

	return result, nil
}

// Native HASH is deliberately nonunique. Always recheck full original bytes,
// validate raw/derived/envelope bindings, and reject a second exact match.
func (tx *Tx) resolveRow(ctx context.Context, key Key, def familyDef, values [][]byte) (*physicalRow, error) {
	r, err := tx.q.query(ctx, "SELECT "+pgSelection(key.Family, def)+" FROM "+tx.connection.table(def.table)+
		" WHERE logical_key=$1 LIMIT 2", stampKey(values))
	if err != nil {
		return nil, tx.fail(databaseError(ctx, "resolve row", err))
	}
	defer r.Close()

	var result *physicalRow
	for r.Next() {
		if result != nil {
			return nil, tx.fail(corruptRow())
		}

		row, err := scanPhysical(r, key.Family, def)
		if err != nil {
			return nil, tx.fail(rowError(ctx, err))
		}

		_, stored, err := row.Key.values()
		if err != nil || !bytes.Equal(stampKey(stored), stampKey(values)) {
			return nil, tx.fail(corruptRow())
		}

		result = &row
	}

	if err := r.Err(); err != nil {
		return nil, tx.fail(databaseError(ctx, "resolve row", err))
	}

	if err := r.Close(); err != nil {
		return nil, tx.fail(databaseError(ctx, "close rows", err))
	}

	return result, nil
}

func (tx *Tx) pgGet(ctx context.Context, key Key, def familyDef, values [][]byte) ([]byte, error) {
	row, err := tx.resolveRow(ctx, key, def, values)
	if err != nil {
		return nil, err
	}

	if _, err := tx.resolveStamp(ctx, key.Family, stampKey(values)); err != nil {
		return nil, err
	}

	if row == nil {
		return nil, memory.PermanentError(memory.CodeNotFound, "canonical SQL row not found", nil)
	}

	return row.Envelope, nil
}

func (tx *Tx) pgPut(
	ctx context.Context, key Key, def familyDef, values [][]byte, encoded []byte, extra [][]byte,
	expected *uint64, cas bool,
) error {
	row, err := tx.resolveRow(ctx, key, def, values)
	if err != nil {
		return err
	}

	stamp, err := tx.resolveStamp(ctx, key.Family, stampKey(values))
	if err != nil {
		return err
	}

	if cas && (expected == nil && row != nil || expected != nil && row == nil) {
		return tx.fail(conflict())
	}

	statement, vals := pgPutStatement(tx.connection.table(def.table), key.Family, def, values, encoded, extra,
		row, expected, cas)

	count, err := tx.q.exec(ctx, statement, vals...)
	if err != nil {
		return tx.fail(databaseError(ctx, "put", err))
	}

	if count != 1 {
		return tx.fail(conflict())
	}

	return tx.pgStamp(ctx, key.Family, stampKey(values), false, stamp)
}

func (tx *Tx) pgDelete(ctx context.Context, key Key, def familyDef, values [][]byte) error {
	row, err := tx.resolveRow(ctx, key, def, values)
	if err != nil {
		return err
	}

	stamp, err := tx.resolveStamp(ctx, key.Family, stampKey(values))
	if err != nil {
		return err
	}

	if row != nil {
		count, err := tx.q.exec(ctx, "DELETE FROM "+tx.connection.table(def.table)+" WHERE row_id=$1", row.id)
		if err != nil {
			return tx.fail(databaseError(ctx, "delete", err))
		}

		if count != 1 {
			return tx.fail(corruptRow())
		}
	}

	return tx.pgStamp(ctx, key.Family, stampKey(values), true, stamp)
}

func pgPutStatement(
	table string, family Family, def familyDef, values [][]byte, encoded []byte, extra [][]byte,
	row *physicalRow, expected *uint64, cas bool,
) (string, []any) {
	cols := append(def.columns(), derivedColumns(family, def)...)
	vals := append(arguments(values), arguments(derivedValues(family, def, values, extra))...)
	cols = append(cols, "envelope")
	cols = append(cols, extraColumns(family)...)
	vals = append(vals, encoded)
	vals = append(vals, arguments(extra)...)

	statement := "INSERT INTO " + table + " (" + strings.Join(cols, ",") +
		") VALUES (" + placeholders(1, len(vals)) + ")"
	if row != nil {
		sets := make([]string, len(cols))
		for i, col := range cols {
			sets[i] = col + "=$" + strconv.Itoa(i+1)
		}

		vals = append(vals, row.id)

		statement = "UPDATE " + table + " SET " + strings.Join(sets, ",") +
			" WHERE row_id=$" + strconv.Itoa(len(vals))
		if cas {
			vals = append(vals, Uint64(*expected))
			statement += " AND version_u64=$" + strconv.Itoa(len(vals))
		}
	}

	return statement, vals
}
