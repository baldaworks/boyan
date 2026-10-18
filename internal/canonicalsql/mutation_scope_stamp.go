package canonicalsql

import "context"

// A live scope cursor must have exactly one live stamp. Reading only its exact
// key keeps corrupt/duplicate/tombstoned cursors from being silently repaired by CAS.
func validateScopeStateStamp(ctx context.Context, tx *Tx, key Key, live bool) error {
	_, values, err := key.values()
	if err != nil {
		return tx.fail(err)
	}

	frame := stampKey(values)

	clock, err := tx.scopeStampClock(ctx, frame)
	if err != nil {
		return err
	}

	r, err := tx.q.query(ctx, "SELECT last_seq,deleted FROM "+tx.connection.table("row_stamp")+
		" WHERE family=$1 AND binary_key=$2 LIMIT 2", []byte(ScopeState), frame)
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

		if err := validateLiveScopeStamp(ctx, r, live, clock); err != nil {
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

func validateLiveScopeStamp(ctx context.Context, r rows, live bool, clock uint64) error {
	var (
		sequence []byte
		deleted  bool
	)
	if err := r.Scan(&sequence, &deleted); err != nil {
		return databaseError(ctx, "scope stamp", err)
	}

	seq, err := DecodeUint64(sequence)
	if err != nil || seq == 0 || seq > clock || live == deleted {
		return corruptRow()
	}

	return nil
}

// Only an exact scope stamp successfully written by this Tx may use its lazy
// sequence. A certificate write must not legitimize an already-future scope.
func (tx *Tx) scopeStampClock(ctx context.Context, frame []byte) (uint64, error) {
	encoded := tx.sequence
	if encoded == nil {
		if err := scanOne(ctx, tx.q, "SELECT mutation_clock FROM "+tx.connection.table("store_meta")+
			" WHERE singleton=1", nil, &encoded); err != nil {
			return 0, tx.fail(databaseError(ctx, "scope stamp clock", err))
		}
	}

	clock, err := DecodeUint64(encoded)
	if err != nil {
		return 0, tx.fail(corruptRow())
	}

	if tx.sequence != nil {
		if _, written := tx.scopeStamps[string(frame)]; !written {
			return clock - 1, nil
		}
	}

	return clock, nil
}

func (tx *Tx) recordScopeStamp(family Family, frame []byte) {
	if family != ScopeState {
		return
	}

	if tx.scopeStamps == nil {
		tx.scopeStamps = make(map[string]struct{})
	}

	tx.scopeStamps[string(frame)] = struct{}{}
}
