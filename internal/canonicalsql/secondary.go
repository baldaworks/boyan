package canonicalsql

import (
	"context"
	"strconv"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// RevisionCursor retains the raw, unsigned revision and opaque ID order.
type RevisionCursor struct {
	Revision uint64
	ID       []byte
}

// MessagesBySource reads a bounded exact source partition in raw message-ID order.
func (tx *Tx) MessagesBySource(
	ctx context.Context, scope memory.Scope, source, after []byte, limit int,
) ([]Row, error) {
	return tx.secondaryPage(ctx, Message, scope, source, after, nil, limit)
}

// RevisionsByItem reads a bounded exact item partition in (revision, ID) order.
func (tx *Tx) RevisionsByItem(
	ctx context.Context, scope memory.Scope, item []byte, after *RevisionCursor, limit int,
) ([]Row, error) {
	if err := tx.check(ctx, false); err != nil {
		return nil, err
	}

	var id, seq []byte

	if after != nil {
		if err := validatePart("id", after.ID); err != nil {
			return nil, tx.fail(err)
		}

		id = after.ID
		seq = Uint64(after.Revision)
	}

	return tx.secondaryPage(ctx, Revision, scope, item, id, seq, limit)
}

func (tx *Tx) secondaryPage(
	ctx context.Context, f Family, scope memory.Scope, partition, after, seq []byte, limit int,
) ([]Row, error) {
	if err := tx.check(ctx, false); err != nil {
		return nil, err
	}

	def, _ := familyDefinition(f)

	where, args, err := rangePredicate(Range{Family: f, Scope: &scope, Limit: limit}, def)
	if err != nil {
		return nil, tx.fail(err)
	}

	if err := validatePart("id", partition); err != nil {
		return nil, tx.fail(err)
	}

	column, hash := "source_id", "source_partition"
	order := []string{"id"}

	if f == Revision {
		column = "item_id"
		hash = "item_partition"
		order = []string{"revision_u64", "id"}
	}

	args = append(args, partition)
	where = addPredicate(where, column+"=$3")

	if after != nil {
		if err := validatePart("id", after); err != nil {
			return nil, tx.fail(err)
		}

		if f == Revision {
			args = append(args, seq, after)
			where = addPredicate(where, "(revision_u64,id)>($4,$5)")
		} else {
			args = append(args, after)
			where = addPredicate(where, "id>$4")
		}
	}

	if tx.connection.pg != nil {
		args = append(args, stampKey([][]byte{[]byte(scope.Key), []byte(scope.Kind), partition}))
		where = addPredicate(where, hash+"=$"+strconv.Itoa(len(args)))

		return tx.pgPage(ctx, f, def, where, args, order, limit)
	}

	return tx.sqliteSecondaryPage(ctx, f, def, where, args, order, limit)
}

func (tx *Tx) sqliteSecondaryPage(
	ctx context.Context, f Family, def familyDef, where string, args []any, order []string, limit int,
) ([]Row, error) {
	statement := "SELECT " + strings.Join(def.columns(), ",") + "," + valueSelection(f) +
		" FROM " + tx.connection.table(def.table) + where +
		" ORDER BY " + strings.Join(order, ",") + " LIMIT $" + strconv.Itoa(len(args)+1)
	args = append(args, limit)

	r, err := tx.q.query(ctx, statement, args...)
	if err != nil {
		return nil, tx.fail(databaseError(ctx, "secondary page", err))
	}
	defer r.Close()

	var result []Row

	for r.Next() {
		row, err := scanRow(r, f, def)
		if err != nil {
			return nil, tx.fail(rowError(ctx, err))
		}

		result = append(result, row)
	}

	if err := r.Err(); err != nil {
		return nil, tx.fail(databaseError(ctx, "secondary page", err))
	}

	if err := r.Close(); err != nil {
		return nil, tx.fail(databaseError(ctx, "close page", err))
	}

	return result, nil
}

// HasLegacyOperationNamespace is an opaque namespace-existence guard, not a
// record decoder. A malformed/nonhex 64-byte suffix still occupies the namespace.
func (tx *Tx) HasLegacyOperationNamespace(ctx context.Context, scope memory.Scope) (bool, error) {
	if err := tx.check(ctx, false); err != nil {
		return false, err
	}

	def, _ := familyDefinition(Operation)

	where, args, err := rangePredicate(Range{Family: Operation, Scope: &scope, Limit: 1}, def)
	if err != nil {
		return false, tx.fail(err)
	}

	if tx.connection.pg != nil {
		args = append(args, stampKey([][]byte{[]byte(scope.Key), []byte(scope.Kind)}))
		where = addPredicate(where, "scope_partition=$3 AND length(id)=91 AND "+
			"substring(id from 1 for 27)='boyan-derived:v1:operation:'::bytea")
	} else {
		args = append(args, []byte("boyan-derived:v1:operation:"))
		where = addPredicate(where, "length(id)=91 AND substr(id,1,27)=$3")
	}

	var found bool

	err = scanOne(ctx, tx.q, "SELECT EXISTS(SELECT 1 FROM "+tx.connection.table(def.table)+where+" LIMIT 1)", args, &found)
	if err != nil {
		return false, tx.fail(databaseError(ctx, "legacy namespace", err))
	}

	return found, nil
}
