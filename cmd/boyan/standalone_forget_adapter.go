package main

import (
	"context"
	"fmt"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/badger"
)

const (
	forgetBatchLimit = 512
	scrubBatchLimit  = 4096
)

type canonicalForgetAdapter struct {
	store *badger.CanonicalStore
}

func newCanonicalForgetAdapter(store *badger.CanonicalStore) (*canonicalForgetAdapter, error) {
	if store == nil {
		return nil, fmt.Errorf("canonical forget store is required")
	}
	return &canonicalForgetAdapter{store: store}, nil
}

func (a *canonicalForgetAdapter) ForgetSource(ctx context.Context, command memory.ForgetSourceCommand) (memory.ForgetOutcome, error) {
	if err := command.Validate(); err != nil {
		return memory.ForgetOutcome{}, err
	}
	sourceID, err := a.store.CanonicalSourceID(ctx, command.Source.Scope, command.Source)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}
	operationID, err := memory.CanonicalForgetOperationID(memory.ForgetKindSource, command.Source.Scope, command.Source, "")
	if err != nil {
		return memory.ForgetOutcome{}, err
	}
	if outcome, found, err := a.store.LoadCanonicalForgetOutcome(ctx, command.Source.Scope, operationID, memory.ForgetKindSource); err != nil || found {
		if err != nil {
			return memory.ForgetOutcome{}, err
		}
		if scrubErr := a.scrubForgetOutcome(ctx, command.Source.Scope, []string{sourceID}, outcome.Revisions); scrubErr != nil {
			return outcome, scrubErr
		}
		return outcome, nil
	}
	scopeState, err := a.store.LoadScopeState(ctx, command.Source.Scope)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}
	var revisionIDs []string
	cursor := ""
	for {
		result, err := memory.DenyCanonicalSourceBatch(ctx, a.store, memory.CanonicalSourceForgetBatchRequest{
			Scope:           command.Source.Scope,
			SourceID:        sourceID,
			AfterRevisionID: cursor,
			Limit:           forgetBatchLimit,
			DeniedAt:        command.ForgottenAt,
		})
		if err != nil {
			return memory.ForgetOutcome{}, err
		}
		revisionIDs = appendUniqueStrings(revisionIDs, result.RevisionIDs)
		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}
	revisions, err := canonicalRevisionRefs(ctx, a.store, command.Source.Scope, revisionIDs)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}
	outcome, err := a.store.CommitCanonicalForget(ctx, memory.CanonicalForgetCommitRequest{
		Scope:                command.Source.Scope,
		OperationID:          operationID,
		Kind:                 memory.ForgetKindSource,
		Fingerprint:          memory.CanonicalForgetFingerprint(operationID, command.Source.Scope, memory.ForgetKindSource, command.Source, ""),
		ExpectedScopeVersion: scopeState.Version,
		Sources:              []memory.SourceRef{command.Source},
		Revisions:            revisions,
		ForgottenAt:          command.ForgottenAt,
	})
	if err != nil {
		return memory.ForgetOutcome{}, err
	}
	if scrubErr := a.scrubForgetOutcome(ctx, command.Source.Scope, []string{sourceID}, outcome.Revisions); scrubErr != nil {
		return outcome, scrubErr
	}
	return outcome, nil
}

func (a *canonicalForgetAdapter) ForgetScope(ctx context.Context, command memory.ForgetScopeCommand) (memory.ForgetOutcome, error) {
	if err := command.Validate(); err != nil {
		return memory.ForgetOutcome{}, err
	}
	operationID, err := memory.CanonicalForgetOperationID(memory.ForgetKindScope, command.Scope, memory.SourceRef{}, command.RequestID)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}
	if outcome, found, err := a.store.LoadCanonicalForgetOutcome(ctx, command.Scope, operationID, memory.ForgetKindScope); err != nil || found {
		if err != nil {
			return memory.ForgetOutcome{}, err
		}
		return outcome, nil
	}
	scopeState, err := a.store.LoadScopeState(ctx, command.Scope)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}
	sources, sourceIDs, err := listCanonicalSources(ctx, a.store, command.Scope)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}
	revisions, revisionIDs, err := listCanonicalRevisions(ctx, a.store, command.Scope)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}
	for _, sourceID := range sourceIDs {
		if err := a.store.DenySource(ctx, command.Scope, sourceID, command.ForgottenAt); err != nil {
			return memory.ForgetOutcome{}, err
		}
	}
	for _, revisionID := range revisionIDs {
		if err := a.store.DenyRevision(ctx, command.Scope, revisionID, command.ForgottenAt); err != nil {
			return memory.ForgetOutcome{}, err
		}
	}
	outcome, err := a.store.CommitCanonicalForget(ctx, memory.CanonicalForgetCommitRequest{
		Scope:                command.Scope,
		OperationID:          operationID,
		Kind:                 memory.ForgetKindScope,
		Fingerprint:          memory.CanonicalForgetFingerprint(operationID, command.Scope, memory.ForgetKindScope, memory.SourceRef{}, command.RequestID),
		ExpectedScopeVersion: scopeState.Version,
		Sources:              sources,
		Revisions:            revisions,
		ForgottenAt:          command.ForgottenAt,
	})
	if err != nil {
		return memory.ForgetOutcome{}, err
	}
	if scrubErr := a.scrubForgetOutcome(ctx, command.Scope, sourceIDs, outcome.Revisions); scrubErr != nil {
		return outcome, scrubErr
	}
	return outcome, nil
}

func (a *canonicalForgetAdapter) scrubForgetOutcome(ctx context.Context, scope memory.Scope, sourceIDs []string, revisions []memory.RevisionRef) error {
	revisionIDs := make([]string, 0, len(revisions))
	for _, revision := range revisions {
		revisionIDs = append(revisionIDs, revision.RevisionID)
	}
	for start := 0; start < len(sourceIDs) || start < len(revisionIDs); start += scrubBatchLimit {
		sourceBatch := chunkStrings(sourceIDs, start, scrubBatchLimit)
		revisionBatch := chunkStrings(revisionIDs, start, scrubBatchLimit)
		if err := a.store.ScrubCanonicalForget(ctx, scope, sourceBatch, revisionBatch); err != nil {
			return err
		}
	}
	return nil
}

func listCanonicalSources(ctx context.Context, store *badger.CanonicalStore, scope memory.Scope) ([]memory.SourceRef, []string, error) {
	sources := make([]memory.SourceRef, 0)
	sourceIDs := make([]string, 0)
	cursor := ""
	for {
		batch, next, err := store.ListCanonicalSourceRefs(ctx, scope, cursor, forgetBatchLimit)
		if err != nil {
			return nil, nil, err
		}
		for _, source := range batch {
			sources = append(sources, source)
			sourceID, err := store.CanonicalSourceID(ctx, scope, source)
			if err != nil {
				return nil, nil, err
			}
			sourceIDs = append(sourceIDs, sourceID)
		}
		if next == "" {
			return sources, sourceIDs, nil
		}
		cursor = next
	}
}

func listCanonicalRevisions(ctx context.Context, store *badger.CanonicalStore, scope memory.Scope) ([]memory.RevisionRef, []string, error) {
	revisions := make([]memory.RevisionRef, 0)
	revisionIDs := make([]string, 0)
	cursor := ""
	for {
		batch, next, err := store.ListCanonicalRevisionRefs(ctx, scope, cursor, forgetBatchLimit)
		if err != nil {
			return nil, nil, err
		}
		revisions = append(revisions, batch...)
		for _, revision := range batch {
			revisionIDs = append(revisionIDs, revision.RevisionID)
		}
		if next == "" {
			return revisions, revisionIDs, nil
		}
		cursor = next
	}
}

func canonicalRevisionRefs(ctx context.Context, store *badger.CanonicalStore, scope memory.Scope, revisionIDs []string) ([]memory.RevisionRef, error) {
	needed := make(map[string]memory.RevisionRef, len(revisionIDs))
	for _, revisionID := range revisionIDs {
		needed[revisionID] = memory.RevisionRef{RevisionID: revisionID}
	}
	loaded, err := loadCanonicalRevisionsByID(ctx, store, scope, needed)
	if err != nil {
		return nil, err
	}
	refs := make([]memory.RevisionRef, 0, len(revisionIDs))
	for _, revisionID := range revisionIDs {
		revision, ok := loaded[revisionID]
		if !ok {
			return nil, fmt.Errorf("canonical revision %s is unavailable", revisionID)
		}
		refs = append(refs, memory.RevisionRef{ItemID: revision.ItemID, RevisionID: revision.RevisionID})
	}
	return refs, nil
}
