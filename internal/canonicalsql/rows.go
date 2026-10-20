package canonicalsql

import (
	"bytes"
	"context"
	"strconv"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func arguments(values [][]byte) []any {
	args := make([]any, len(values))
	for i, value := range values {
		args[i] = value
	}

	return args
}

func placeholders(start, count int) string {
	values := make([]string, count)
	for i := range count {
		values[i] = "$" + strconv.Itoa(start+i)
	}

	return strings.Join(values, ",")
}

func equality(columns []string, start int) string {
	terms := make([]string, len(columns))
	for i, col := range columns {
		terms[i] = col + "=$" + strconv.Itoa(start+i)
	}

	return strings.Join(terms, " AND ")
}

func valueSelection(f Family) string {
	// The CASE prevents a corrupted oversized value from being copied by a driver.
	cols := []string{"CASE WHEN length(envelope)<=4194304 THEN envelope ELSE NULL END", "length(envelope)"}
	cols = append(cols, extraColumns(f)...)

	return strings.Join(cols, ",")
}

func readValue(r interface{ Scan(dest ...any) error }, key Key) ([]byte, error) {
	var (
		encoded []byte
		size    int
	)

	extra := make([][]byte, len(extraColumns(key.Family)))

	dest := []any{&encoded, &size}
	for i := range extra {
		dest = append(dest, &extra[i])
	}

	if err := r.Scan(dest...); err != nil {
		return nil, err
	}

	return validateStoredValue(key, encoded, size, extra)
}

func validateStoredValue(key Key, encoded []byte, size int, extra [][]byte) ([]byte, error) {
	if size > MaxEnvelopeBytes {
		return nil, limitError()
	}

	if len(encoded) != size {
		return nil, corruptRow()
	}

	// These bytes came from storage. The same validation on caller-supplied
	// envelopes remains an input error in columnValues and the public codec.
	expected, err := columnValues(key, encoded)
	if err != nil {
		return nil, corruptRow()
	}

	for i, col := range expected {
		if !bytes.Equal(col, extra[i]) {
			return nil, corruptRow()
		}
	}

	return encoded, nil
}

// Get reads exactly one typed key and validates its bounded stored value.
func (tx *Tx) Get(ctx context.Context, key Key) ([]byte, error) {
	if err := tx.check(ctx, false); err != nil {
		return nil, err
	}

	def, values, err := key.values()
	if err != nil {
		return nil, tx.fail(err)
	}

	if tx.connection.pg != nil {
		return tx.pgGet(ctx, key, def, values)
	}

	r, err := tx.q.query(ctx, "SELECT "+valueSelection(key.Family)+" FROM "+tx.connection.table(def.table)+
		" WHERE "+equality(def.columns(), 1), arguments(values)...)
	if err != nil {
		return nil, tx.fail(databaseError(ctx, "get", err))
	}
	defer r.Close()

	if !r.Next() {
		if err := r.Err(); err != nil {
			return nil, tx.fail(databaseError(ctx, "get", err))
		}
		// Absence is a normal lookup outcome, so it does not poison the transaction.
		return nil, memory.PermanentError(memory.CodeNotFound, "canonical SQL row not found", nil)
	}

	encoded, err := readValue(r, key)
	if err != nil {
		return nil, tx.fail(rowError(ctx, err))
	}

	if err := r.Err(); err != nil {
		return nil, tx.fail(databaseError(ctx, "get", err))
	}

	if err := r.Close(); err != nil {
		return nil, tx.fail(databaseError(ctx, "close rows", err))
	}

	return encoded, nil
}

func rowError(ctx context.Context, err error) error {
	if _, _, ok := memory.ClassifyError(err); ok {
		return err
	}

	return databaseError(ctx, "decode row", err)
}

// Put upserts a validated envelope and its latest stamp. Domain-level immutable
// equality, replay and privacy gates belong in the caller's same transaction.
func (tx *Tx) Put(ctx context.Context, key Key, encoded []byte) error {
	if err := tx.check(ctx, true); err != nil {
		return err
	}

	def, values, err := key.values()
	if err != nil {
		return tx.fail(err)
	}

	extra, err := columnValues(key, encoded)
	if err != nil {
		return tx.fail(err)
	}

	if tx.connection.pg != nil {
		return tx.pgPut(ctx, key, def, values, encoded, extra, nil, false)
	}

	columns := append(def.columns(), "envelope")
	columns = append(columns, extraColumns(key.Family)...)
	args := append(arguments(values), encoded)
	args = append(args, arguments(extra)...)

	updates := []string{"envelope=excluded.envelope"}
	for _, col := range extraColumns(key.Family) {
		updates = append(updates, col+"=excluded."+col)
	}

	_, err = tx.q.exec(ctx, "INSERT INTO "+tx.connection.table(def.table)+" ("+strings.Join(columns, ",")+") VALUES ("+
		placeholders(1, len(args))+") ON CONFLICT("+strings.Join(def.columns(), ",")+") DO UPDATE SET "+
		strings.Join(updates, ","), args...)
	if err != nil {
		return tx.fail(databaseError(ctx, "put", err))
	}

	return nil
}

// Delete removes a typed key; absence is harmless.
func (tx *Tx) Delete(ctx context.Context, key Key) error {
	if err := tx.check(ctx, true); err != nil {
		return err
	}

	def, values, err := key.values()
	if err != nil {
		return tx.fail(err)
	}

	if tx.connection.pg != nil {
		return tx.pgDelete(ctx, key, def, values)
	}

	statement := "DELETE FROM " + tx.connection.table(def.table) + " WHERE " + equality(def.columns(), 1)

	_, err = tx.q.exec(ctx, statement, arguments(values)...)
	if err != nil {
		return tx.fail(databaseError(ctx, "delete", err))
	}

	return nil
}

// CompareAndSwapScope inserts an absent scope when expected is nil; otherwise it
// conditionally updates the encoded expected version. It does not choose the
// next domain version/change sequence.
func (tx *Tx) CompareAndSwapScope(ctx context.Context, expected *uint64, state memory.ScopeState) error {
	if err := tx.check(ctx, true); err != nil {
		return err
	}

	key := Key{Family: ScopeState, Scope: state.Scope}

	def, values, err := key.values()
	if err != nil {
		return tx.fail(err)
	}

	encoded, err := Encode(ScopeState, state)
	if err != nil {
		return tx.fail(err)
	}

	extra, err := columnValues(key, encoded)
	if err != nil {
		return tx.fail(err)
	}

	if tx.connection.pg != nil {
		return tx.pgPut(ctx, key, def, values, encoded, extra, expected, true)
	}

	if err := tx.validateScopeCAS(ctx, key, expected); err != nil {
		return err
	}

	table := tx.connection.table(def.table)

	args := append(arguments(values), encoded, extra[0], extra[1])
	statement := "INSERT INTO " + table +
		" (scope_key,scope_kind,envelope,version_u64,change_seq_u64) VALUES($1,$2,$3,$4,$5) " +
		"ON CONFLICT(scope_key,scope_kind) DO NOTHING"

	if expected != nil {
		args = append(args, Uint64(*expected))
		statement = "UPDATE " + table + " SET envelope=$3,version_u64=$4,change_seq_u64=$5 " +
			"WHERE scope_key=$1 AND scope_kind=$2 AND version_u64=$6"
	}

	count, err := tx.q.exec(ctx, statement, args...)
	if err != nil {
		return tx.fail(databaseError(ctx, "scope compare-and-swap", err))
	}

	if count != 1 {
		return tx.fail(conflict())
	}

	return nil
}

func (tx *Tx) validateScopeCAS(ctx context.Context, key Key, expected *uint64) error {
	// Validate the existing envelope/columns before trusting its CAS column.
	existing, err := tx.Get(ctx, key)

	code, _, _ := memory.ClassifyError(err)
	if err != nil && code != memory.CodeNotFound {
		return err
	}

	if expected == nil && existing != nil || expected != nil && existing == nil {
		return tx.fail(conflict())
	}

	return nil
}
