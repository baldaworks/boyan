package canonicalsql

import (
	"context"
)

type physicalStamp struct {
	id       int64
	sequence []byte
	deleted  bool
}

func (tx *Tx) resolveStamp(ctx context.Context, family Family, key []byte) (*physicalStamp, error) {
	r, err := tx.q.query(ctx, "SELECT stamp_id,last_seq,deleted FROM "+tx.connection.table("row_stamp")+
		" WHERE family=$1 AND binary_key=$2 LIMIT 2", []byte(family), key)
	if err != nil {
		return nil, tx.fail(databaseError(ctx, "resolve stamp", err))
	}
	defer r.Close()

	var result *physicalStamp
	for r.Next() {
		if result != nil {
			return nil, tx.fail(corruptRow())
		}

		var stamp physicalStamp
		if err := r.Scan(&stamp.id, &stamp.sequence, &stamp.deleted); err != nil {
			return nil, tx.fail(databaseError(ctx, "resolve stamp", err))
		}

		if _, err := DecodeUint64(stamp.sequence); err != nil {
			return nil, tx.fail(corruptRow())
		}

		result = &stamp
	}

	if err := r.Err(); err != nil {
		return nil, tx.fail(databaseError(ctx, "resolve stamp", err))
	}

	if err := r.Close(); err != nil {
		return nil, tx.fail(databaseError(ctx, "close stamp", err))
	}

	return result, nil
}

func (tx *Tx) pgStamp(ctx context.Context, family Family, key []byte, deleted bool, stamp *physicalStamp) error {
	seq, err := tx.mutationSequence(ctx)
	if err != nil {
		return err
	}

	statement := "INSERT INTO " + tx.connection.table("row_stamp") +
		" (family,binary_key,last_seq,deleted) VALUES($1,$2,$3,$4)"
	args := []any{[]byte(family), key, seq, deleted}

	if stamp != nil {
		statement = "UPDATE " + tx.connection.table("row_stamp") + " SET last_seq=$1,deleted=$2 WHERE stamp_id=$3"
		args = []any{seq, deleted, stamp.id}
	}

	count, err := tx.q.exec(ctx, statement, args...)
	if err != nil {
		return tx.fail(databaseError(ctx, "stamp row", err))
	}

	if count != 1 {
		return tx.fail(corruptRow())
	}

	return nil
}
