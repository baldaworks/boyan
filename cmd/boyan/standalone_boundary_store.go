package main

import (
	"context"
	"fmt"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/badger"
)

type canonicalBoundaryStore struct {
	store  *badger.CanonicalStore
	reader *badger.CanonicalReader
}

func newCanonicalBoundaryStore(store *badger.CanonicalStore, reader *badger.CanonicalReader) (*canonicalBoundaryStore, error) {
	if store == nil || reader == nil {
		return nil, fmt.Errorf("canonical boundary store dependencies are required")
	}
	return &canonicalBoundaryStore{store: store, reader: reader}, nil
}

func (s *canonicalBoundaryStore) LookupOperation(ctx context.Context, lookup memory.OperationLookup) (memory.OperationLookupResult, error) {
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

func (s *canonicalBoundaryStore) LoadScope(ctx context.Context, scope memory.Scope) (memory.ScopeSnapshot, error) {
	view, err := s.reader.LoadCanonicalBoundaryView(ctx, scope)
	if err != nil {
		return memory.ScopeSnapshot{}, err
	}
	return view.Snapshot, nil
}

func (s *canonicalBoundaryStore) Commit(ctx context.Context, request memory.CommitRequest) (memory.OperationOutcome, error) {
	if err := request.Validate(); err != nil {
		return memory.OperationOutcome{}, err
	}
	if len(request.Scenarios)+len(request.Profiles) == 0 {
		return s.commitCanonicalOperation(ctx, request)
	}
	view, err := s.reader.LoadCanonicalBoundaryView(ctx, request.Scope)
	if err != nil {
		return memory.OperationOutcome{}, err
	}
	mutation, err := s.buildBoundaryMutation(ctx, request, view)
	if err != nil {
		return memory.OperationOutcome{}, err
	}
	outcome, err := s.store.ApplyCanonicalMutation(ctx, mutation)
	if err != nil {
		return memory.OperationOutcome{}, err
	}
	return memory.OperationOutcome{
		SchemaVersion: memory.DerivedSchemaVersionV1,
		OperationID:   request.OperationID,
		Stage:         request.Stage,
		Scope:         request.Scope,
		ScopeVersion:  outcome.ScopeVersion,
		Revisions:     collectOperationRevisions(request),
	}, nil
}

func (s *canonicalBoundaryStore) commitCanonicalOperation(ctx context.Context, request memory.CommitRequest) (memory.OperationOutcome, error) {
	committedAt := operationCommittedAt(request)
	outcome, err := s.store.CommitCanonicalOperation(ctx, memory.CanonicalOperationCommitRequest{
		Scope:                request.Scope,
		ExpectedScopeVersion: request.ExpectedScopeVersion,
		OperationID:          request.OperationID,
		Fingerprint:          stableStandaloneFingerprint("boundary-operation", request),
		CommittedAt:          committedAt,
	})
	if err != nil {
		return memory.OperationOutcome{}, err
	}
	return memory.OperationOutcome{
		SchemaVersion: memory.DerivedSchemaVersionV1,
		OperationID:   request.OperationID,
		Stage:         request.Stage,
		Scope:         request.Scope,
		ScopeVersion:  outcome.ScopeVersion,
	}, nil
}
