package canonicalsql

import (
	"context"
	"encoding/binary"
	"strconv"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// StampCursor is the exact archive traversal order, independent of physical IDs.
// BinaryKey is a versioned full tuple frame, not a domain row-order cursor.
type StampCursor struct {
	Sequence  uint64
	Family    Family
	BinaryKey []byte
}

// Stamp is a latest mutation and, for a live row, its validated envelope.
type Stamp struct {
	Cursor   StampCursor
	Key      Key
	Deleted  bool
	Envelope []byte
}

const frameLengthBytes = 4

func keyFromFrame(f Family, frame []byte) (Key, error) {
	def, err := familyDefinition(f)
	if err != nil || len(frame) == 0 || frame[0] != 1 {
		return Key{}, corruptRow()
	}

	remaining := frame[1:]

	values := make([][]byte, 0, len(def.columns()))
	for range def.columns() {
		if len(remaining) < frameLengthBytes {
			return Key{}, corruptRow()
		}

		size := uint64(binary.BigEndian.Uint32(remaining))

		remaining = remaining[frameLengthBytes:]
		if size > uint64(len(remaining)) {
			return Key{}, corruptRow()
		}

		values = append(values, remaining[:size])
		remaining = remaining[size:]
	}

	if len(remaining) != 0 {
		return Key{}, corruptRow()
	}

	return keyFromValues(f, def, values)
}

// ScanStamps bounds client memory and envelope hydration. The server may sort
// tied sequence/family key sets; the full binary key is never a B-tree column.
func (tx *Tx) ScanStamps(ctx context.Context, after *StampCursor, limit int) ([]Stamp, error) {
	return tx.scanStampsSince(ctx, 0, after, limit)
}

func (tx *Tx) scanStampsSince(ctx context.Context, since uint64, after *StampCursor, limit int) ([]Stamp, error) {
	if err := tx.check(ctx, false); err != nil {
		return nil, err
	}

	if limit <= 0 || limit > memory.MaxCanonicalReadRecords {
		return nil, tx.fail(limitError())
	}

	statement, args, err := tx.stampPageStatementSince(since, after, limit)
	if err != nil {
		return nil, tx.fail(err)
	}

	result, err := tx.readStampPage(ctx, statement, args)
	if err != nil {
		return nil, err
	}

	for i := range result {
		if err := tx.hydrateStamp(ctx, &result[i]); err != nil {
			return nil, err
		}
	}

	return result, nil
}

func (tx *Tx) readStampPage(ctx context.Context, statement string, args []any) ([]Stamp, error) {
	r, err := tx.q.query(ctx, statement, args...)
	if err != nil {
		return nil, tx.fail(databaseError(ctx, "stamp page", err))
	}
	defer r.Close()

	var result []Stamp

	for r.Next() {
		var (
			seq, family, key []byte
			deleted          bool
		)
		if err := r.Scan(&seq, &family, &key, &deleted); err != nil {
			return nil, tx.fail(databaseError(ctx, "stamp page", err))
		}

		sequence, err := DecodeUint64(seq)
		if err != nil || sequence == 0 {
			return nil, tx.fail(corruptRow())
		}

		identity, err := keyFromFrame(Family(family), key)
		if err != nil {
			return nil, tx.fail(err)
		}

		cursor := StampCursor{Sequence: sequence, Family: Family(family), BinaryKey: key}
		result = append(result, Stamp{Cursor: cursor, Key: identity, Deleted: deleted})
	}

	if err := r.Err(); err != nil {
		return nil, tx.fail(databaseError(ctx, "stamp page", err))
	}

	if err := r.Close(); err != nil {
		return nil, tx.fail(databaseError(ctx, "close stamp page", err))
	}

	return result, nil
}

func (tx *Tx) hydrateStamp(ctx context.Context, stamp *Stamp) error {
	encoded, err := tx.Get(ctx, stamp.Key)

	code, _, _ := memory.ClassifyError(err)
	if stamp.Deleted && code == memory.CodeNotFound {
		return nil
	}

	if err != nil && code != memory.CodeNotFound {
		return err
	}

	if err != nil || stamp.Deleted {
		return tx.fail(corruptRow())
	}

	stamp.Envelope = encoded

	return nil
}

func (tx *Tx) stampPageStatement(after *StampCursor, limit int) (string, []any, error) {
	return tx.stampPageStatementSince(0, after, limit)
}

func (tx *Tx) stampPageStatementSince(since uint64, after *StampCursor, limit int) (string, []any, error) {
	where := ""

	var args []any
	if since != 0 {
		args = []any{Uint64(since)}
		where = " WHERE last_seq>$1"
	}

	if after != nil {
		if _, err := keyFromFrame(after.Family, after.BinaryKey); err != nil {
			return "", nil, invalid("invalid canonical stamp cursor")
		}

		args = []any{Uint64(after.Sequence), []byte(after.Family), after.BinaryKey}
		where = " WHERE (last_seq,family,binary_key)>($1,$2,$3)"
	}

	table := tx.connection.table("row_stamp")

	statement := "SELECT last_seq,family,binary_key,deleted FROM " + table + where +
		" ORDER BY last_seq,family,binary_key LIMIT $" + strconv.Itoa(len(args)+1)
	if tx.connection.pg != nil {
		statement = "WITH stamp_page AS MATERIALIZED (SELECT stamp_id,last_seq,family,binary_key FROM " + table + where +
			" ORDER BY last_seq,family,binary_key LIMIT $" + strconv.Itoa(len(args)+1) +
			") SELECT p.last_seq,p.family,p.binary_key,s.deleted FROM stamp_page p JOIN " + table +
			" s ON s.stamp_id=p.stamp_id ORDER BY p.last_seq,p.family,p.binary_key"
	}

	args = append(args, limit)

	return statement, args, nil
}
