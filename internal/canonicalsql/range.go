package canonicalsql

import (
	"bytes"
	"context"
	"math"
	"strconv"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Range selects a bounded family page. Scope nil enumerates a whole family for
// maintenance. Prefix fixes leading Parts within an exact scope/global family.
// After is an exclusive complete key, never a length-framed sorting key.
type Range struct {
	Family Family
	Scope  *memory.Scope
	Prefix [][]byte
	After  *Key
	Limit  int
}

// Row is a validated key and envelope from one snapshot.
type Row struct {
	Key      Key
	Envelope []byte
}

// Scan returns at most Limit rows in native binary tuple order.
func (tx *Tx) Scan(ctx context.Context, request Range) ([]Row, error) {
	if err := tx.check(ctx, false); err != nil {
		return nil, err
	}

	def, err := familyDefinition(request.Family)
	if err != nil {
		return nil, tx.fail(err)
	}

	where, args, err := rangePredicate(request, def)
	if err != nil {
		return nil, tx.fail(err)
	}

	columns := def.columns()
	if tx.connection.pg != nil {
		where, args = pgPartitionPredicate(request, def, where, args)

		return tx.pgPage(ctx, request.Family, def, where, args, columns, request.Limit)
	}

	statement := "SELECT " + strings.Join(columns, ",") + "," + valueSelection(request.Family) + " FROM " +
		tx.connection.table(def.table) + where + " ORDER BY " + strings.Join(columns, ",") +
		" LIMIT $" + strconv.Itoa(len(args)+1)
	args = append(args, request.Limit)

	r, err := tx.q.query(ctx, statement, args...)
	if err != nil {
		return nil, tx.fail(databaseError(ctx, "scan", err))
	}
	defer r.Close()

	var result []Row

	for r.Next() {
		row, err := scanRow(r, request.Family, def)
		if err != nil {
			return nil, tx.fail(rowError(ctx, err))
		}

		result = append(result, row)
	}

	if err := r.Err(); err != nil {
		return nil, tx.fail(databaseError(ctx, "scan", err))
	}

	if err := r.Close(); err != nil {
		return nil, tx.fail(databaseError(ctx, "close rows", err))
	}

	return result, nil
}

func rangePredicate(request Range, def familyDef) (string, []any, error) {
	if err := validateRange(request, def); err != nil {
		return "", nil, err
	}

	var (
		args       []any
		conditions []string
	)

	if request.Scope != nil {
		if err := request.Scope.Validate(); err != nil {
			return "", nil, err
		}

		if len(request.Scope.Key) > math.MaxUint16 {
			return "", nil, limitError()
		}

		args = append(args, []byte(request.Scope.Key), []byte(request.Scope.Kind))
		conditions = append(conditions, "scope_key=$1 AND scope_kind=$2")
	}

	for i, part := range request.Prefix {
		if err := validatePart(def.parts[i], part); err != nil {
			return "", nil, err
		}

		args = append(args, part)
		conditions = append(conditions, def.parts[i]+"=$"+strconv.Itoa(len(args)))
	}

	if request.After != nil {
		condition, values, err := cursorPredicate(request, def, len(args)+1)
		if err != nil {
			return "", nil, err
		}

		conditions = append(conditions, condition)
		args = append(args, arguments(values)...)
	}

	where := ""
	if len(conditions) != 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	return where, args, nil
}

func scanRow(r rows, family Family, def familyDef) (Row, error) {
	values := make([][]byte, len(def.columns()))
	extra := make([][]byte, len(extraColumns(family)))

	var (
		encoded []byte
		size    int
	)

	dest := make([]any, 0, len(values)+len(extra)+valueColumnCount)
	for i := range values {
		dest = append(dest, &values[i])
	}

	dest = append(dest, &encoded, &size)
	for i := range extra {
		dest = append(dest, &extra[i])
	}

	if err := r.Scan(dest...); err != nil {
		return Row{}, err
	}

	key := Key{Family: family}
	if def.scoped {
		key.Scope = memory.Scope{Key: string(values[0]), Kind: memory.ScopeKind(values[1])}
		key.Parts = values[2:]
	} else if len(def.parts) != 0 {
		key.Parts = values
	} else if len(values[0]) != 0 {
		return Row{}, invalid("invalid canonical singleton")
	}

	if _, _, err := key.values(); err != nil {
		return Row{}, err
	}

	encoded, err := validateStoredValue(key, encoded, size, extra)
	if err != nil {
		return Row{}, err
	}

	return Row{Key: key, Envelope: encoded}, nil
}

const valueColumnCount = 2

func validateRange(request Range, def familyDef) error {
	if request.Limit <= 0 || request.Limit > memory.MaxCanonicalReadRecords {
		return limitError()
	}

	if len(request.Prefix) > len(def.parts) {
		return invalid("invalid canonical range prefix")
	}

	if !def.scoped && request.Scope != nil || def.scoped && request.Scope == nil && len(request.Prefix) != 0 {
		return invalid("invalid canonical range scope")
	}

	return nil
}

func cursorPredicate(request Range, def familyDef, start int) (string, [][]byte, error) {
	after := request.After
	if after.Family != request.Family || request.Scope != nil && after.Scope != *request.Scope {
		return "", nil, invalid("canonical range cursor belongs to another partition")
	}

	_, values, err := after.values()
	if err != nil {
		return "", nil, err
	}

	for i, part := range request.Prefix {
		if !bytes.Equal(after.Parts[i], part) {
			return "", nil, invalid("canonical range cursor does not match prefix")
		}
	}

	cols := def.columns()
	if len(cols) == 1 {
		return cols[0] + " > $" + strconv.Itoa(start), values, nil
	}

	return "(" + strings.Join(cols, ",") + ") > (" + placeholders(start, len(values)) + ")", values, nil
}
