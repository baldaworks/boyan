package canonicalops

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (s *CanonicalBoundaryStore) LookupOperation(
	ctx context.Context,
	lookup memory.OperationLookup,
) (
	memory.OperationLookupResult,
	error,
) {
	if err := lookup.Validate(); err != nil {
		return memory.OperationLookupResult{}, err
	}

	record, found, err := s.store.LoadCanonicalOperation(ctx, lookup.Scope, lookup.OperationID)
	if err != nil || !found {
		return memory.OperationLookupResult{}, err
	}

	revisions, err := s.lookupOperationRevisions(ctx, lookup.Scope, record.Outcome.RevisionIDs)
	if err != nil {
		return memory.OperationLookupResult{}, err
	}

	outcome := memory.OperationOutcome{
		SchemaVersion: memory.DerivedSchemaVersionV1,
		OperationID:   lookup.OperationID,
		Stage:         lookup.Stage,
		Scope:         lookup.Scope,
		ScopeVersion:  record.Outcome.ScopeVersion,
		Revisions:     revisions,
	}

	return memory.OperationLookupResult{Found: true, Outcome: outcome}, nil
}
