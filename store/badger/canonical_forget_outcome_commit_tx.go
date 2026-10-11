package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func (s *CanonicalStore) commitCanonicalForgetOutcome(
	txn *badger.Txn,
	request memory.CanonicalForgetCommitRequest,
	key []byte,
	scopeKey []byte,
) (memory.ForgetOutcome, error) {
	if outcome, ok, err := loadCanonicalForgetOutcome(txn, request, key); ok || err != nil {
		return outcome, err
	}

	state, err := loadCanonicalForgetScopeState(txn, request.Scope, scopeKey)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}

	if state.Version != request.ExpectedScopeVersion {
		return memory.ForgetOutcome{}, memory.RetryableError(
			memory.CodeConflict,
			"canonical forget scope version changed",
			nil,
		)
	}

	state.Version++
	state.ChangeSeq++

	outcome := buildCanonicalForgetOutcome(request, state)
	if err := outcome.Validate(); err != nil {
		return memory.ForgetOutcome{}, err
	}

	if err := writeCanonicalForgetOutcome(txn, request, key, scopeKey, state, outcome); err != nil {
		return memory.ForgetOutcome{}, err
	}

	if s.beforeCanonicalMutationCommit != nil {
		if err := s.beforeCanonicalMutationCommit(); err != nil {
			return memory.ForgetOutcome{}, err
		}
	}

	return outcome, nil
}

func buildCanonicalForgetOutcome(
	request memory.CanonicalForgetCommitRequest,
	state memory.ScopeState,
) memory.ForgetOutcome {
	return memory.ForgetOutcome{
		SchemaVersion: memory.DerivedSchemaVersionV1,
		OperationID:   request.OperationID,
		Kind:          request.Kind,
		Scope:         request.Scope,
		ScopeVersion:  state.Version,
		Sources:       append([]memory.SourceRef(nil), request.Sources...),
		Revisions:     append([]memory.RevisionRef(nil), request.Revisions...),
	}
}
