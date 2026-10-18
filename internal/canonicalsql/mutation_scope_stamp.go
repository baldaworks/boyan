package canonicalsql

import "context"

// A live scope cursor must have exactly one live stamp. Reading only its exact
// key keeps corrupt/duplicate/tombstoned cursors from being silently repaired by CAS.
func validateScopeStateStamp(ctx context.Context, tx *Tx, key Key, live bool) error {
	_, values, err := key.values()
	if err != nil {
		return tx.fail(err)
	}

	r, err := tx.q.query(ctx, "SELECT last_seq,deleted FROM "+tx.connection.table("row_stamp")+
		" WHERE family=$1 AND binary_key=$2 LIMIT 2", []byte(ScopeState), stampKey(values))
	if err != nil {
		return tx.fail(databaseError(ctx, "scope stamp", err))
	}
	defer r.Close()

	found := false
	for r.Next() {
		if found {
			return tx.fail(corruptRow())
		}

		found = true

		if err := validateLiveScopeStamp(ctx, r, live); err != nil {
			return tx.fail(err)
		}
	}

	if err := r.Err(); err != nil {
		return tx.fail(databaseError(ctx, "scope stamp", err))
	}

	if live && !found {
		return tx.fail(corruptRow())
	}

	return nil
}

func validateLiveScopeStamp(ctx context.Context, r rows, live bool) error {
	var (
		sequence []byte
		deleted  bool
	)
	if err := r.Scan(&sequence, &deleted); err != nil {
		return databaseError(ctx, "scope stamp", err)
	}

	seq, err := DecodeUint64(sequence)
	if err != nil || seq == 0 || live == deleted {
		return corruptRow()
	}

	return nil
}
