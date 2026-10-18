package canonicalsql

import "context"

// hasRangeRows checks the raw exclusive range without hydrating a lookahead
// envelope. In particular a full page still hydrates at most its requested limit.
func (tx *Tx) hasRangeRows(ctx context.Context, request Range) (bool, error) {
	if err := tx.check(ctx, false); err != nil {
		return false, err
	}

	def, err := familyDefinition(request.Family)
	if err != nil {
		return false, tx.fail(err)
	}

	where, args, err := rangePredicate(request, def)
	if err != nil {
		return false, tx.fail(err)
	}

	if tx.connection.pg != nil {
		where, args = pgPartitionPredicate(request, def, where, args)
	}

	var found bool

	err = scanOne(ctx, tx.q, "SELECT EXISTS(SELECT 1 FROM "+tx.connection.table(def.table)+where+" LIMIT 1)", args, &found)
	if err != nil {
		return false, tx.fail(databaseError(ctx, "range existence", err))
	}

	return found, nil
}
