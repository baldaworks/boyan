package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/baldaworks/boyan"
	"github.com/baldaworks/boyan/store/badger"
)

const (
	forgetBatchLimit = 512
	scrubBatchLimit  = 4096
)

type canonicalBoundaryStore struct {
	store  *badger.BadgerSessionMemoryStore
	reader *badger.CanonicalReader
}

func newCanonicalBoundaryStore(store *badger.BadgerSessionMemoryStore, reader *badger.CanonicalReader) (*canonicalBoundaryStore, error) {
	if store == nil || reader == nil {
		return nil, fmt.Errorf("canonical boundary store dependencies are required")
	}
	return &canonicalBoundaryStore{store: store, reader: reader}, nil
}

func (s *canonicalBoundaryStore) LookupOperation(ctx context.Context, lookup boyan.OperationLookup) (boyan.OperationLookupResult, error) {
	if err := lookup.Validate(); err != nil {
		return boyan.OperationLookupResult{}, err
	}
	record, found, err := s.store.LoadCanonicalOperation(ctx, lookup.Scope, lookup.OperationID)
	if err != nil || !found {
		return boyan.OperationLookupResult{}, err
	}
	revisions, err := s.lookupOperationRevisions(ctx, lookup.Scope, record.Outcome.RevisionIDs)
	if err != nil {
		return boyan.OperationLookupResult{}, err
	}
	outcome := boyan.OperationOutcome{
		SchemaVersion: boyan.DerivedSchemaVersionV1,
		OperationID:   lookup.OperationID,
		Stage:         lookup.Stage,
		Scope:         lookup.Scope,
		ScopeVersion:  record.Outcome.ScopeVersion,
		Revisions:     revisions,
	}
	return boyan.OperationLookupResult{Found: true, Outcome: outcome}, nil
}

func (s *canonicalBoundaryStore) LoadScope(ctx context.Context, scope boyan.Scope) (boyan.ScopeSnapshot, error) {
	view, err := s.reader.LoadCanonicalBoundaryView(ctx, scope)
	if err != nil {
		return boyan.ScopeSnapshot{}, err
	}
	return view.Snapshot, nil
}

func (s *canonicalBoundaryStore) Commit(ctx context.Context, request boyan.CommitRequest) (boyan.OperationOutcome, error) {
	if err := request.Validate(); err != nil {
		return boyan.OperationOutcome{}, err
	}
	if len(request.Scenarios)+len(request.Profiles) == 0 {
		return s.commitCanonicalOperation(ctx, request)
	}
	view, err := s.reader.LoadCanonicalBoundaryView(ctx, request.Scope)
	if err != nil {
		return boyan.OperationOutcome{}, err
	}
	mutation, err := s.buildBoundaryMutation(ctx, request, view)
	if err != nil {
		return boyan.OperationOutcome{}, err
	}
	outcome, err := s.store.ApplyCanonicalMutation(ctx, mutation)
	if err != nil {
		return boyan.OperationOutcome{}, err
	}
	return boyan.OperationOutcome{
		SchemaVersion: boyan.DerivedSchemaVersionV1,
		OperationID:   request.OperationID,
		Stage:         request.Stage,
		Scope:         request.Scope,
		ScopeVersion:  outcome.ScopeVersion,
		Revisions:     collectOperationRevisions(request),
	}, nil
}

func (s *canonicalBoundaryStore) commitCanonicalOperation(ctx context.Context, request boyan.CommitRequest) (boyan.OperationOutcome, error) {
	committedAt := operationCommittedAt(request)
	outcome, err := s.store.CommitCanonicalOperation(ctx, boyan.CanonicalOperationCommitRequest{
		Scope:                request.Scope,
		ExpectedScopeVersion: request.ExpectedScopeVersion,
		OperationID:          request.OperationID,
		Fingerprint:          stableStandaloneFingerprint("boundary-operation", request),
		CommittedAt:          committedAt,
	})
	if err != nil {
		return boyan.OperationOutcome{}, err
	}
	return boyan.OperationOutcome{
		SchemaVersion: boyan.DerivedSchemaVersionV1,
		OperationID:   request.OperationID,
		Stage:         request.Stage,
		Scope:         request.Scope,
		ScopeVersion:  outcome.ScopeVersion,
	}, nil
}

func (s *canonicalBoundaryStore) buildBoundaryMutation(ctx context.Context, request boyan.CommitRequest, view boyan.CanonicalBoundaryView) (boyan.CanonicalMutation, error) {
	parentRefs, evidence, err := s.translateBoundaryParents(ctx, request.Scope, view.LegacyToCanonical, request)
	if err != nil {
		return boyan.CanonicalMutation{}, err
	}
	if len(evidence) == 0 {
		return boyan.CanonicalMutation{}, fmt.Errorf("boundary commit requires grounded parent evidence")
	}
	existingItems := make(map[string]struct{}, len(view.Snapshot.Scenarios)+len(view.Snapshot.Profiles))
	for _, scenario := range view.Snapshot.Scenarios {
		existingItems[scenario.Meta.ItemID] = struct{}{}
	}
	for _, profile := range view.Snapshot.Profiles {
		existingItems[profile.Meta.ItemID] = struct{}{}
	}

	mutation := boyan.CanonicalMutation{
		SchemaVersion:        boyan.CanonicalSchemaVersionV1,
		Scope:                request.Scope,
		ExpectedScopeVersion: request.ExpectedScopeVersion,
		Operation: boyan.OperationRecord{
			OperationID: request.OperationID,
			Fingerprint: stableStandaloneFingerprint("boundary-mutation", request),
			CommittedAt: operationCommittedAt(request),
		},
	}
	for _, scenario := range request.Scenarios {
		payload, payloadRef, err := compatibilityPayloadRef("scenario", scenario.Meta.RevisionID, boyan.CanonicalCompatibilityPayload{
			SchemaVersion:     boyan.CanonicalCompatibilitySchemaVersion,
			Kind:              boyan.DerivedKindScenario,
			TopicKey:          scenario.TopicKey,
			Title:             scenario.Title,
			Text:              scenario.Summary,
			LegacyItemID:      scenario.Meta.ItemID,
			LegacyRevisionID:  scenario.Meta.RevisionID,
			LegacyOperationID: scenario.Meta.OperationID,
			LegacyParents:     append([]boyan.RevisionRef(nil), scenario.Meta.Provenance.ParentRevisions...),
			Supersedes:        cloneOptionalRef(scenario.Meta.Supersedes),
		})
		if err != nil {
			return boyan.CanonicalMutation{}, err
		}
		if _, exists := existingItems[scenario.Meta.ItemID]; !exists {
			mutation.Items = append(mutation.Items, boyan.MemoryItem{
				ItemID:    scenario.Meta.ItemID,
				Scope:     request.Scope,
				Kind:      boyan.MemoryKindState,
				MemoryKey: derivedMemoryKey(scenario.Meta.ItemID),
			})
			existingItems[scenario.Meta.ItemID] = struct{}{}
		}
		mutation.Revisions = append(mutation.Revisions, boyan.MemoryRevision{
			SchemaVersion: boyan.MemorySchemaVersionV2,
			RevisionID:    scenario.Meta.RevisionID,
			ItemID:        scenario.Meta.ItemID,
			Revision:      scenario.Meta.Revision,
			Parents:       revisionIDs(parentRefs[scenario.Meta.RevisionID]),
			Temporal:      boyan.Temporal{ObservedAt: scenario.Meta.CreatedAt},
			Evidence:      append([]boyan.EvidenceRef(nil), evidence[scenario.Meta.RevisionID]...),
			Sensitivity:   boyan.SensitivityStandard,
			Retention:     boyan.RetentionClassStandard,
			Payload:       payloadRef,
		})
		mutation.Heads = append(mutation.Heads, boyan.ItemHead{ItemID: scenario.Meta.ItemID, RevisionID: scenario.Meta.RevisionID})
		mutation.Payloads = append(mutation.Payloads, payload)
		mutation.Operation.Outcome = append(mutation.Operation.Outcome, scenario.Meta.RevisionID)
	}
	for _, profile := range request.Profiles {
		payload, payloadRef, err := compatibilityPayloadRef("profile", profile.Meta.RevisionID, boyan.CanonicalCompatibilityPayload{
			SchemaVersion:     boyan.CanonicalCompatibilitySchemaVersion,
			Kind:              boyan.DerivedKindProfile,
			Text:              profile.Summary,
			LegacyItemID:      profile.Meta.ItemID,
			LegacyRevisionID:  profile.Meta.RevisionID,
			LegacyOperationID: profile.Meta.OperationID,
			LegacyParents:     append([]boyan.RevisionRef(nil), profile.Meta.Provenance.ParentRevisions...),
			Supersedes:        cloneOptionalRef(profile.Meta.Supersedes),
		})
		if err != nil {
			return boyan.CanonicalMutation{}, err
		}
		if _, exists := existingItems[profile.Meta.ItemID]; !exists {
			mutation.Items = append(mutation.Items, boyan.MemoryItem{
				ItemID:    profile.Meta.ItemID,
				Scope:     request.Scope,
				Kind:      boyan.MemoryKindState,
				MemoryKey: derivedMemoryKey(profile.Meta.ItemID),
			})
			existingItems[profile.Meta.ItemID] = struct{}{}
		}
		mutation.Revisions = append(mutation.Revisions, boyan.MemoryRevision{
			SchemaVersion: boyan.MemorySchemaVersionV2,
			RevisionID:    profile.Meta.RevisionID,
			ItemID:        profile.Meta.ItemID,
			Revision:      profile.Meta.Revision,
			Parents:       revisionIDs(parentRefs[profile.Meta.RevisionID]),
			Temporal:      boyan.Temporal{ObservedAt: profile.Meta.CreatedAt},
			Evidence:      append([]boyan.EvidenceRef(nil), evidence[profile.Meta.RevisionID]...),
			Sensitivity:   boyan.SensitivityStandard,
			Retention:     boyan.RetentionClassStandard,
			Payload:       payloadRef,
		})
		mutation.Heads = append(mutation.Heads, boyan.ItemHead{ItemID: profile.Meta.ItemID, RevisionID: profile.Meta.RevisionID})
		mutation.Payloads = append(mutation.Payloads, payload)
		mutation.Operation.Outcome = append(mutation.Operation.Outcome, profile.Meta.RevisionID)
	}
	if err := mutation.Validate(); err != nil {
		return boyan.CanonicalMutation{}, err
	}
	return mutation, nil
}

func (s *canonicalBoundaryStore) translateBoundaryParents(ctx context.Context, scope boyan.Scope, mapping map[boyan.RevisionRef]boyan.RevisionRef, request boyan.CommitRequest) (map[string][]boyan.RevisionRef, map[string][]boyan.EvidenceRef, error) {
	parentSets := make(map[string][]boyan.RevisionRef, len(request.Scenarios)+len(request.Profiles))
	needed := make(map[string]boyan.RevisionRef)
	translate := func(ref boyan.RevisionRef) (boyan.RevisionRef, error) {
		if canonical, ok := mapping[ref]; ok {
			return canonical, nil
		}
		if err := ref.Validate(); err != nil {
			return boyan.RevisionRef{}, err
		}
		return ref, nil
	}
	addParents := func(revisionID string, parents []boyan.RevisionRef) error {
		translated := make([]boyan.RevisionRef, 0, len(parents))
		for _, parent := range parents {
			canonical, err := translate(parent)
			if err != nil {
				return err
			}
			translated = appendUniqueRefs(translated, canonical)
			needed[canonical.RevisionID] = canonical
		}
		parentSets[revisionID] = translated
		return nil
	}
	for _, scenario := range request.Scenarios {
		if err := addParents(scenario.Meta.RevisionID, scenario.Meta.Provenance.ParentRevisions); err != nil {
			return nil, nil, err
		}
	}
	for _, profile := range request.Profiles {
		if err := addParents(profile.Meta.RevisionID, profile.Meta.Provenance.ParentRevisions); err != nil {
			return nil, nil, err
		}
	}
	if len(needed) == 0 {
		return nil, nil, fmt.Errorf("boundary commit requires parent revisions")
	}
	loaded, err := loadCanonicalRevisionsByID(ctx, s.store, scope, needed)
	if err != nil {
		return nil, nil, err
	}
	evidenceSets := make(map[string][]boyan.EvidenceRef, len(parentSets))
	for revisionID, parents := range parentSets {
		seen := make(map[string]struct{})
		evidence := make([]boyan.EvidenceRef, 0, len(parents))
		for _, parent := range parents {
			revision, ok := loaded[parent.RevisionID]
			if !ok {
				return nil, nil, fmt.Errorf("canonical parent revision %s is unavailable", parent.RevisionID)
			}
			for _, ref := range revision.Evidence {
				key := evidenceKey(ref)
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
				evidence = append(evidence, ref)
			}
		}
		if len(evidence) == 0 {
			return nil, nil, fmt.Errorf("boundary commit parent evidence is empty")
		}
		if len(evidence) > boyan.MaxSourcesPerRevision {
			return nil, nil, fmt.Errorf("boundary commit evidence exceeds %d references", boyan.MaxSourcesPerRevision)
		}
		evidenceSets[revisionID] = evidence
	}
	return parentSets, evidenceSets, nil
}

func (s *canonicalBoundaryStore) lookupOperationRevisions(ctx context.Context, scope boyan.Scope, revisionIDs []string) ([]boyan.RevisionRef, error) {
	if len(revisionIDs) == 0 {
		return nil, nil
	}
	needed := make(map[string]boyan.RevisionRef, len(revisionIDs))
	for _, revisionID := range revisionIDs {
		needed[revisionID] = boyan.RevisionRef{RevisionID: revisionID}
	}
	loaded, err := loadCanonicalRevisionsByID(ctx, s.store, scope, needed)
	if err != nil {
		return nil, err
	}
	refs := make([]boyan.RevisionRef, 0, len(revisionIDs))
	for _, revisionID := range revisionIDs {
		compatibility, found, err := s.reader.LoadCanonicalCompatibility(ctx, scope, revisionID)
		if err != nil {
			return nil, err
		}
		if found {
			refs = append(refs, boyan.RevisionRef{
				ItemID:     compatibility.LegacyItemID,
				RevisionID: compatibility.LegacyRevisionID,
			})
			continue
		}
		revision, ok := loaded[revisionID]
		if !ok {
			return nil, fmt.Errorf("canonical replay revision %s is unavailable", revisionID)
		}
		refs = append(refs, boyan.RevisionRef{ItemID: revision.ItemID, RevisionID: revision.RevisionID})
	}
	return refs, nil
}

type canonicalForgetAdapter struct {
	store *badger.BadgerSessionMemoryStore
}

func newCanonicalForgetAdapter(store *badger.BadgerSessionMemoryStore) (*canonicalForgetAdapter, error) {
	if store == nil {
		return nil, fmt.Errorf("canonical forget store is required")
	}
	return &canonicalForgetAdapter{store: store}, nil
}

func (a *canonicalForgetAdapter) ForgetSource(ctx context.Context, command boyan.ForgetSourceCommand) (boyan.ForgetOutcome, error) {
	if err := command.Validate(); err != nil {
		return boyan.ForgetOutcome{}, err
	}
	sourceID, err := a.store.CanonicalSourceID(ctx, command.Source.Scope, command.Source)
	if err != nil {
		return boyan.ForgetOutcome{}, err
	}
	operationID, err := boyan.CanonicalForgetOperationID(boyan.ForgetKindSource, command.Source.Scope, command.Source, "")
	if err != nil {
		return boyan.ForgetOutcome{}, err
	}
	if outcome, found, err := a.store.LoadCanonicalForgetOutcome(ctx, command.Source.Scope, operationID, boyan.ForgetKindSource); err != nil || found {
		if err != nil {
			return boyan.ForgetOutcome{}, err
		}
		if scrubErr := a.scrubForgetOutcome(ctx, command.Source.Scope, []string{sourceID}, outcome.Revisions); scrubErr != nil {
			return outcome, scrubErr
		}
		return outcome, nil
	}
	scopeState, err := a.store.LoadScopeState(ctx, command.Source.Scope)
	if err != nil {
		return boyan.ForgetOutcome{}, err
	}
	var revisionIDs []string
	cursor := ""
	for {
		result, err := boyan.DenyCanonicalSourceBatch(ctx, a.store, boyan.CanonicalSourceForgetBatchRequest{
			Scope:           command.Source.Scope,
			SourceID:        sourceID,
			AfterRevisionID: cursor,
			Limit:           forgetBatchLimit,
			DeniedAt:        command.ForgottenAt,
		})
		if err != nil {
			return boyan.ForgetOutcome{}, err
		}
		revisionIDs = appendUniqueStrings(revisionIDs, result.RevisionIDs)
		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}
	revisions, err := canonicalRevisionRefs(ctx, a.store, command.Source.Scope, revisionIDs)
	if err != nil {
		return boyan.ForgetOutcome{}, err
	}
	outcome, err := a.store.CommitCanonicalForget(ctx, boyan.CanonicalForgetCommitRequest{
		Scope:                command.Source.Scope,
		OperationID:          operationID,
		Kind:                 boyan.ForgetKindSource,
		Fingerprint:          boyan.CanonicalForgetFingerprint(operationID, command.Source.Scope, boyan.ForgetKindSource, command.Source, ""),
		ExpectedScopeVersion: scopeState.Version,
		Sources:              []boyan.SourceRef{command.Source},
		Revisions:            revisions,
		ForgottenAt:          command.ForgottenAt,
	})
	if err != nil {
		return boyan.ForgetOutcome{}, err
	}
	if scrubErr := a.scrubForgetOutcome(ctx, command.Source.Scope, []string{sourceID}, outcome.Revisions); scrubErr != nil {
		return outcome, scrubErr
	}
	return outcome, nil
}

func (a *canonicalForgetAdapter) ForgetScope(ctx context.Context, command boyan.ForgetScopeCommand) (boyan.ForgetOutcome, error) {
	if err := command.Validate(); err != nil {
		return boyan.ForgetOutcome{}, err
	}
	operationID, err := boyan.CanonicalForgetOperationID(boyan.ForgetKindScope, command.Scope, boyan.SourceRef{}, command.RequestID)
	if err != nil {
		return boyan.ForgetOutcome{}, err
	}
	if outcome, found, err := a.store.LoadCanonicalForgetOutcome(ctx, command.Scope, operationID, boyan.ForgetKindScope); err != nil || found {
		if err != nil {
			return boyan.ForgetOutcome{}, err
		}
		return outcome, nil
	}
	scopeState, err := a.store.LoadScopeState(ctx, command.Scope)
	if err != nil {
		return boyan.ForgetOutcome{}, err
	}
	sources, sourceIDs, err := listCanonicalSources(ctx, a.store, command.Scope)
	if err != nil {
		return boyan.ForgetOutcome{}, err
	}
	revisions, revisionIDs, err := listCanonicalRevisions(ctx, a.store, command.Scope)
	if err != nil {
		return boyan.ForgetOutcome{}, err
	}
	for _, sourceID := range sourceIDs {
		if err := a.store.DenySource(ctx, command.Scope, sourceID, command.ForgottenAt); err != nil {
			return boyan.ForgetOutcome{}, err
		}
	}
	for _, revisionID := range revisionIDs {
		if err := a.store.DenyRevision(ctx, command.Scope, revisionID, command.ForgottenAt); err != nil {
			return boyan.ForgetOutcome{}, err
		}
	}
	outcome, err := a.store.CommitCanonicalForget(ctx, boyan.CanonicalForgetCommitRequest{
		Scope:                command.Scope,
		OperationID:          operationID,
		Kind:                 boyan.ForgetKindScope,
		Fingerprint:          boyan.CanonicalForgetFingerprint(operationID, command.Scope, boyan.ForgetKindScope, boyan.SourceRef{}, command.RequestID),
		ExpectedScopeVersion: scopeState.Version,
		Sources:              sources,
		Revisions:            revisions,
		ForgottenAt:          command.ForgottenAt,
	})
	if err != nil {
		return boyan.ForgetOutcome{}, err
	}
	if scrubErr := a.scrubForgetOutcome(ctx, command.Scope, sourceIDs, outcome.Revisions); scrubErr != nil {
		return outcome, scrubErr
	}
	return outcome, nil
}

func (a *canonicalForgetAdapter) scrubForgetOutcome(ctx context.Context, scope boyan.Scope, sourceIDs []string, revisions []boyan.RevisionRef) error {
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

func listCanonicalSources(ctx context.Context, store *badger.BadgerSessionMemoryStore, scope boyan.Scope) ([]boyan.SourceRef, []string, error) {
	sources := make([]boyan.SourceRef, 0)
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

func listCanonicalRevisions(ctx context.Context, store *badger.BadgerSessionMemoryStore, scope boyan.Scope) ([]boyan.RevisionRef, []string, error) {
	revisions := make([]boyan.RevisionRef, 0)
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

func canonicalRevisionRefs(ctx context.Context, store *badger.BadgerSessionMemoryStore, scope boyan.Scope, revisionIDs []string) ([]boyan.RevisionRef, error) {
	needed := make(map[string]boyan.RevisionRef, len(revisionIDs))
	for _, revisionID := range revisionIDs {
		needed[revisionID] = boyan.RevisionRef{RevisionID: revisionID}
	}
	loaded, err := loadCanonicalRevisionsByID(ctx, store, scope, needed)
	if err != nil {
		return nil, err
	}
	refs := make([]boyan.RevisionRef, 0, len(revisionIDs))
	for _, revisionID := range revisionIDs {
		revision, ok := loaded[revisionID]
		if !ok {
			return nil, fmt.Errorf("canonical revision %s is unavailable", revisionID)
		}
		refs = append(refs, boyan.RevisionRef{ItemID: revision.ItemID, RevisionID: revision.RevisionID})
	}
	return refs, nil
}

func loadCanonicalRevisionsByID(ctx context.Context, store *badger.BadgerSessionMemoryStore, scope boyan.Scope, needed map[string]boyan.RevisionRef) (map[string]boyan.MemoryRevision, error) {
	if len(needed) == 0 {
		return map[string]boyan.MemoryRevision{}, nil
	}
	ids := make([]string, 0, len(needed))
	for revisionID := range needed {
		ids = append(ids, revisionID)
	}
	loaded := make(map[string]boyan.MemoryRevision, len(ids))
	for start := 0; start < len(ids); start += boyan.MaxRecallCandidates {
		end := minInt(len(ids), start+boyan.MaxRecallCandidates)
		revisions, err := store.LoadCanonicalRevisions(ctx, boyan.CanonicalRevisionReadRequest{
			Scope:       scope,
			RevisionIDs: ids[start:end],
		})
		if err != nil {
			return nil, err
		}
		for _, revision := range revisions {
			loaded[revision.RevisionID] = revision
		}
	}
	return loaded, nil
}

func compatibilityPayloadRef(kind, identity string, payload boyan.CanonicalCompatibilityPayload) (boyan.CanonicalPayload, boyan.PayloadRef, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return boyan.CanonicalPayload{}, boyan.PayloadRef{}, fmt.Errorf("encode %s compatibility payload: %w", kind, err)
	}
	return canonicalPayload("compatibility:"+kind+":"+identity, data)
}

func canonicalPayload(identity string, data []byte) (boyan.CanonicalPayload, boyan.PayloadRef, error) {
	if len(data) == 0 {
		return boyan.CanonicalPayload{}, boyan.PayloadRef{}, fmt.Errorf("canonical payload data is empty")
	}
	digest := sha256.Sum256(data)
	ref := boyan.PayloadRef{
		ID:       stableStandaloneID("payload", identity),
		Digest:   hex.EncodeToString(digest[:]),
		ByteSize: uint32(len(data)),
	}
	payload := boyan.CanonicalPayload{Ref: ref, Data: append([]byte(nil), data...)}
	if err := payload.Validate(); err != nil {
		return boyan.CanonicalPayload{}, boyan.PayloadRef{}, err
	}
	return payload, ref, nil
}

func collectOperationRevisions(request boyan.CommitRequest) []boyan.RevisionRef {
	revisions := make([]boyan.RevisionRef, 0, len(request.Atoms)+len(request.Scenarios)+len(request.Profiles))
	for _, atom := range request.Atoms {
		revisions = append(revisions, boyan.RevisionRef{ItemID: atom.Meta.ItemID, RevisionID: atom.Meta.RevisionID})
	}
	for _, scenario := range request.Scenarios {
		revisions = append(revisions, boyan.RevisionRef{ItemID: scenario.Meta.ItemID, RevisionID: scenario.Meta.RevisionID})
	}
	for _, profile := range request.Profiles {
		revisions = append(revisions, boyan.RevisionRef{ItemID: profile.Meta.ItemID, RevisionID: profile.Meta.RevisionID})
	}
	return revisions
}

func operationCommittedAt(request boyan.CommitRequest) time.Time {
	for _, scenario := range request.Scenarios {
		if !scenario.Meta.CreatedAt.IsZero() {
			return scenario.Meta.CreatedAt
		}
	}
	for _, profile := range request.Profiles {
		if !profile.Meta.CreatedAt.IsZero() {
			return profile.Meta.CreatedAt
		}
	}
	for _, atom := range request.Atoms {
		if !atom.Meta.CreatedAt.IsZero() {
			return atom.Meta.CreatedAt
		}
	}
	return time.Now().UTC()
}

func stableStandaloneFingerprint(kind string, value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return stableStandaloneID(kind, "marshal-error")
	}
	return stableStandaloneID(kind, string(data))
}

func stableStandaloneID(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		hash.Write([]byte{0})
		hash.Write([]byte(part))
	}
	return "boyan-standalone:" + hex.EncodeToString(hash.Sum(nil))
}

func evidenceKey(ref boyan.EvidenceRef) string {
	return strings.Join([]string{
		ref.SourceID,
		ref.MessageID,
		string(ref.Role),
		fmt.Sprintf("%d", ref.StartByte),
		fmt.Sprintf("%d", ref.EndByte),
		string(ref.AssertionMode),
		ref.TextDigest,
	}, ":")
}

func appendUniqueRefs(values []boyan.RevisionRef, ref boyan.RevisionRef) []boyan.RevisionRef {
	for _, existing := range values {
		if existing == ref {
			return values
		}
	}
	return append(values, ref)
}

func appendUniqueStrings(values []string, next []string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	for _, value := range next {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}

func revisionIDs(refs []boyan.RevisionRef) []string {
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		ids = append(ids, ref.RevisionID)
	}
	return ids
}

func cloneOptionalRef(ref *boyan.RevisionRef) *boyan.RevisionRef {
	if ref == nil {
		return nil
	}
	copyOf := *ref
	return &copyOf
}

func derivedMemoryKey(itemID string) boyan.MemoryKey {
	return boyan.MemoryKey("derived:" + itemID)
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func chunkStrings(values []string, start, size int) []string {
	if start >= len(values) {
		return nil
	}
	end := minInt(len(values), start+size)
	return values[start:end]
}
