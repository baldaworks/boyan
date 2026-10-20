package canonicalsql

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

var _ memory.CanonicalScopeStatePager = (*Owner)(nil)

// SourceRevisionBatch reads only the exact source's exclusive descendant page.
func (o *Owner) SourceRevisionBatch(ctx context.Context, scope memory.Scope, sourceID, after string,
	limit uint32) ([]string, string, error) {
	if err := mutationContext(ctx); err != nil {
		return nil, "", err
	}

	if err := scope.Validate(); err != nil {
		return nil, "", err
	}

	if sourceID == "" || limit == 0 || limit > 512 {
		return nil, "", limitError()
	}

	var ids []string

	err := o.View(ctx, func(ctx context.Context, tx *Tx) error {
		var err error

		ids, err = scanReaderSource(ctx, tx, scope, sourceID, after, int(limit))

		return err
	})
	if err != nil {
		return nil, "", err
	}

	if len(ids) == 0 {
		return ids, "", nil
	}

	return ids, ids[len(ids)-1], nil
}

func scanReaderSource(ctx context.Context, tx *Tx, scope memory.Scope, sourceID, after string,
	limit int) ([]string, error) {
	scan := Range{Family: SourceRevision, Scope: &scope, Prefix: [][]byte{[]byte(sourceID)}}
	if after != "" {
		scan.After = &Key{Family: SourceRevision, Scope: scope, Parts: [][]byte{[]byte(sourceID), []byte(after)}}
	}

	ids := make([]string, 0, limit)
	for len(ids) < limit {
		scan.Limit = min(limit-len(ids), memory.MaxCanonicalReadRecords)

		rows, err := tx.Scan(ctx, scan)
		if err != nil {
			return nil, err
		}

		for _, row := range rows {
			var edge mutationProvenanceEdge
			if err := Decode(SourceRevision, row.Envelope, &edge); err != nil {
				return nil, err
			}

			if edge.ParentRevisionID != sourceID || edge.ChildRevisionID != string(row.Key.Parts[1]) {
				return nil, corruptRow()
			}

			ids = append(ids, edge.ChildRevisionID)
			key := row.Key
			scan.After = &key
		}

		if len(rows) < scan.Limit {
			break
		}
	}

	return ids, nil
}

// ListCanonicalScopeStates uses an opaque framed cursor only to recover the
// original tuple. Native pages order by the raw scope key and kind.
func (o *Owner) ListCanonicalScopeStates(ctx context.Context, cursor []byte, limit int) ([]memory.ScopeState,
	[]byte, error) {
	if err := mutationContext(ctx); err != nil {
		return nil, nil, err
	}

	if limit < 1 || limit > memory.MaxCanonicalReadRecords {
		return nil, nil, limitError()
	}

	scan := Range{Family: ScopeState, Limit: limit}
	if len(cursor) > 0 {
		key, err := keyFromFrame(ScopeState, cursor)
		if err != nil {
			return nil, nil, err
		}

		scan.After = &key
	}

	var (
		states []memory.ScopeState
		next   []byte
	)

	err := o.View(ctx, func(ctx context.Context, tx *Tx) error {
		var err error

		states, next, err = scanReaderScopes(ctx, tx, scan)

		return err
	})
	if err != nil {
		return nil, nil, err
	}

	return states, next, nil
}

func scanReaderScopes(ctx context.Context, tx *Tx, scan Range) ([]memory.ScopeState, []byte, error) {
	var (
		states []memory.ScopeState
		next   []byte
	)

	rows, err := tx.Scan(ctx, scan)
	if err != nil {
		return nil, nil, err
	}

	for _, row := range rows {
		var state memory.ScopeState
		if err := Decode(ScopeState, row.Envelope, &state); err != nil {
			return nil, nil, err
		}

		if state.Validate() != nil || state.Scope != row.Key.Scope {
			return nil, nil, corruptRow()
		}

		states = append(states, state)
	}

	if len(rows) < scan.Limit {
		return states, next, nil
	}

	last := rows[len(rows)-1].Key

	more, err := tx.hasRangeRows(ctx, Range{Family: ScopeState, After: &last, Limit: 1})
	if err != nil {
		return nil, nil, err
	}

	if more {
		_, values, _ := last.values()
		next = stampKey(values)
	}

	return states, next, nil
}
