package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// ListPendingScopeErases pages only owned, nonterminal fences in raw tuple order.
func (o *Owner) ListPendingScopeErases(ctx context.Context, cursor []byte,
	limit int) ([]memory.ScopeEraseJournal, []byte, error) {
	if limit < 1 || limit > memory.MaxCanonicalReadRecords {
		return nil, nil, limitError()
	}

	scan := Range{Family: EraseFence, Limit: limit}
	if len(cursor) > 0 {
		key, err := keyFromFrame(EraseFence, cursor)
		if err != nil {
			return nil, nil, err
		}

		scan.After = &key
	}

	var (
		result []memory.ScopeEraseJournal
		next   []byte
	)

	err := o.View(ctx, func(ctx context.Context, tx *Tx) error {
		rows, err := tx.Scan(ctx, scan)
		if err != nil {
			return err
		}

		for _, row := range rows {
			j, err := pendingEraseRecord(ctx, tx, row)
			if err != nil {
				return err
			}

			result = append(result, j)
		}

		next, err = pendingEraseCursor(ctx, tx, scan, rows)

		return err
	})
	if err != nil {
		return nil, nil, err
	}

	return result, next, nil
}

func pendingEraseRecord(ctx context.Context, tx *Tx, row Row) (memory.ScopeEraseJournal, error) {
	if string(row.Key.Parts[0]) != "pending" {
		return memory.ScopeEraseJournal{}, corruptRow()
	}

	var fence scopeEraseFence
	if err := Decode(EraseFence, row.Envelope, &fence); err != nil {
		return memory.ScopeEraseJournal{}, err
	}

	j, found, err := loadScopeEraseTxn(ctx, tx, row.Key.Scope, fence.OperationID)
	if err != nil {
		return j, err
	}

	if !found || eraseTerminal(j.Stage) {
		return j, corruptRow()
	}

	return j, validateScopeEraseOwnership(ctx, tx, j)
}

func pendingEraseCursor(ctx context.Context, tx *Tx, scan Range, rows []Row) ([]byte, error) {
	if len(rows) < scan.Limit {
		return nil, nil
	}

	last := rows[len(rows)-1].Key
	scan.After = &last
	scan.Limit = 1

	more, err := tx.hasRangeRows(ctx, scan)
	if err != nil || !more {
		return nil, err
	}

	_, values, _ := last.values()

	return stampKey(values), nil
}
