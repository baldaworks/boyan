package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

var (
	_ memory.CanonicalOperationReader    = (*Owner)(nil)
	_ memory.CanonicalOperationCommitter = (*Owner)(nil)
)

// LoadScopeState reads one exact scope cursor.
func (o *Owner) LoadScopeState(ctx context.Context, scope memory.Scope) (memory.ScopeState, error) {
	if err := mutationContext(ctx); err != nil {
		return memory.ScopeState{}, err
	}

	key, err := proofScopeKey(scope)
	if err != nil {
		return memory.ScopeState{}, err
	}

	var state memory.ScopeState

	err = o.View(ctx, func(ctx context.Context, tx *Tx) error {
		var err error

		state, err = loadProofScopeState(ctx, tx, scope, key)

		return err
	})

	return state, err
}

// ScanScopeChanges reads a bounded exclusive change tail.
func (o *Owner) ScanScopeChanges(ctx context.Context, scope memory.Scope, after uint64,
	limit uint32) ([]memory.ScopeChange, error) {
	if err := mutationContext(ctx); err != nil {
		return nil, err
	}

	if err := scope.Validate(); err != nil {
		return nil, err
	}

	if limit == 0 || limit > 512 {
		return nil, limitError()
	}

	key, err := proofChangeKey(scope, after)
	if err != nil {
		return nil, err
	}

	changes := make([]memory.ScopeChange, 0, limit)

	err = o.View(ctx, func(ctx context.Context, tx *Tx) error {
		var err error

		changes, err = scanMutationChanges(ctx, tx, scope, key, int(limit))

		return err
	})
	if err != nil {
		return nil, err
	}

	return changes, nil
}

// LoadCanonicalRevisions reads the requested immutable revisions in order.
func (o *Owner) LoadCanonicalRevisions(ctx context.Context,
	request memory.CanonicalRevisionReadRequest) ([]memory.MemoryRevision, error) {
	if err := mutationContext(ctx); err != nil {
		return nil, err
	}

	if err := request.Validate(); err != nil {
		return nil, err
	}

	revisions := make([]memory.MemoryRevision, 0, len(request.RevisionIDs))

	err := o.View(ctx, func(ctx context.Context, tx *Tx) error {
		for _, id := range request.RevisionIDs {
			key, err := privacyKey(request.Scope, Revision, id)
			if err != nil {
				return err
			}

			var revision memory.MemoryRevision
			if err := getPrivacyRecord(ctx, tx, key, Revision, &revision); err != nil {
				return err
			}

			if err := revision.Validate(); err != nil {
				return err
			}

			revisions = append(revisions, revision)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return revisions, nil
}

// LoadCanonicalOperation verifies the operation's durable change and scope binding.
func (o *Owner) LoadCanonicalOperation(ctx context.Context, scope memory.Scope,
	id string) (memory.CanonicalOperationRecord, bool, error) {
	if err := mutationContext(ctx); err != nil {
		return memory.CanonicalOperationRecord{}, false, err
	}

	if err := scope.Validate(); err != nil {
		return memory.CanonicalOperationRecord{}, false, err
	}

	if err := (memory.RevisionRef{ItemID: id, RevisionID: id}).Validate(); err != nil {
		return memory.CanonicalOperationRecord{}, false, err
	}

	var (
		record memory.CanonicalOperationRecord
		found  bool
	)

	err := o.View(ctx, func(ctx context.Context, tx *Tx) error {
		var err error

		record, found, err = loadCanonicalOperation(ctx, tx, scope, id)

		return err
	})
	if err != nil {
		return memory.CanonicalOperationRecord{}, false, err
	}

	return record, found, nil
}

func loadCanonicalOperation(ctx context.Context, tx *Tx, scope memory.Scope,
	id string) (memory.CanonicalOperationRecord, bool, error) {
	key, err := privacyKey(scope, Operation, id)
	if err != nil {
		return memory.CanonicalOperationRecord{}, false, err
	}

	var stored canonicalOperation

	err = getPrivacyRecord(ctx, tx, key, Operation, &stored)
	if privacyMissing(err) {
		return memory.CanonicalOperationRecord{}, false, nil
	}

	if err != nil {
		return memory.CanonicalOperationRecord{}, false, invalidStoredCanonicalOutcome(err)
	}

	record := memory.CanonicalOperationRecord{Fingerprint: stored.Fingerprint, Outcome: stored.Outcome}
	if err := record.Validate(); err != nil {
		return memory.CanonicalOperationRecord{}, false, invalidStoredCanonicalOutcome(err)
	}

	if err := validateCanonicalOutcomeBinding(ctx, tx, scope, id, record.Outcome); err != nil {
		return memory.CanonicalOperationRecord{}, false, err
	}

	return record, true, nil
}

func scanMutationChanges(ctx context.Context, tx *Tx, scope memory.Scope,
	key Key, limit int) ([]memory.ScopeChange, error) {
	changes := make([]memory.ScopeChange, 0, limit)
	for len(changes) < int(limit) {
		pageLimit := min(int(limit)-len(changes), memory.MaxCanonicalReadRecords)

		rows, err := tx.Scan(ctx, Range{Family: ScopeChange, Scope: &scope, After: &key, Limit: pageLimit})
		if err != nil {
			return nil, err
		}

		for _, row := range rows {
			var change memory.ScopeChange
			if err := Decode(ScopeChange, row.Envelope, &change); err != nil {
				return nil, err
			}

			if err := change.Validate(); err != nil {
				return nil, err
			}

			changes = append(changes, change)
			key = row.Key
		}

		if len(rows) < pageLimit {
			break
		}
	}

	return changes, nil
}
