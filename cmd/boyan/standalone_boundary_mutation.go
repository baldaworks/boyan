package main

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (s *canonicalBoundaryStore) buildBoundaryMutation(ctx context.Context, request memory.CommitRequest, view memory.CanonicalBoundaryView) (memory.CanonicalMutation, error) {
	parentRefs, evidence, err := s.translateBoundaryParents(ctx, request.Scope, view.LegacyToCanonical, request)
	if err != nil {
		return memory.CanonicalMutation{}, err
	}
	if len(evidence) == 0 {
		return memory.CanonicalMutation{}, errBoundaryEvidenceRequired
	}
	existingItems := existingBoundaryItems(view)

	mutation := memory.CanonicalMutation{
		SchemaVersion:        memory.CanonicalSchemaVersionV1,
		Scope:                request.Scope,
		ExpectedScopeVersion: request.ExpectedScopeVersion,
		Operation: memory.OperationRecord{
			OperationID: request.OperationID,
			Fingerprint: stableStandaloneFingerprint("boundary-mutation", request),
			CommittedAt: operationCommittedAt(request),
		},
	}
	if err := appendBoundaryScenarioMutation(&mutation, request, existingItems, parentRefs, evidence); err != nil {
		return memory.CanonicalMutation{}, err
	}
	if err := appendBoundaryProfileMutation(&mutation, request, existingItems, parentRefs, evidence); err != nil {
		return memory.CanonicalMutation{}, err
	}
	if err := mutation.Validate(); err != nil {
		return memory.CanonicalMutation{}, err
	}
	return mutation, nil
}

func existingBoundaryItems(view memory.CanonicalBoundaryView) map[string]struct{} {
	existingItems := make(map[string]struct{}, len(view.Snapshot.Scenarios)+len(view.Snapshot.Profiles))
	for _, scenario := range view.Snapshot.Scenarios {
		existingItems[scenario.Meta.ItemID] = struct{}{}
	}
	for _, profile := range view.Snapshot.Profiles {
		existingItems[profile.Meta.ItemID] = struct{}{}
	}
	return existingItems
}

func appendBoundaryScenarioMutation(
	mutation *memory.CanonicalMutation,
	request memory.CommitRequest,
	existingItems map[string]struct{},
	parentRefs map[string][]memory.RevisionRef,
	evidence map[string][]memory.EvidenceRef,
) error {
	for _, scenario := range request.Scenarios {
		payload, payloadRef, err := compatibilityPayloadRef("scenario", scenario.Meta.RevisionID, memory.CanonicalCompatibilityPayload{
			SchemaVersion:     memory.CanonicalCompatibilitySchemaVersion,
			Kind:              memory.DerivedKindScenario,
			TopicKey:          scenario.TopicKey,
			Title:             scenario.Title,
			Text:              scenario.Summary,
			LegacyItemID:      scenario.Meta.ItemID,
			LegacyRevisionID:  scenario.Meta.RevisionID,
			LegacyOperationID: scenario.Meta.OperationID,
			LegacyParents:     append([]memory.RevisionRef(nil), scenario.Meta.Provenance.ParentRevisions...),
			Supersedes:        cloneOptionalRef(scenario.Meta.Supersedes),
		})
		if err != nil {
			return err
		}
		ensureBoundaryItem(existingItems, &mutation.Items, request.Scope, scenario.Meta.ItemID)
		mutation.Revisions = append(mutation.Revisions, memory.MemoryRevision{
			SchemaVersion: memory.MemorySchemaVersion,
			RevisionID:    scenario.Meta.RevisionID,
			ItemID:        scenario.Meta.ItemID,
			Revision:      scenario.Meta.Revision,
			Parents:       revisionIDs(parentRefs[scenario.Meta.RevisionID]),
			Temporal:      memory.Temporal{ObservedAt: scenario.Meta.CreatedAt},
			Evidence:      append([]memory.EvidenceRef(nil), evidence[scenario.Meta.RevisionID]...),
			Sensitivity:   memory.SensitivityStandard,
			Retention:     memory.RetentionClassStandard,
			Payload:       payloadRef,
		})
		mutation.Heads = append(mutation.Heads, memory.ItemHead{ItemID: scenario.Meta.ItemID, RevisionID: scenario.Meta.RevisionID})
		mutation.Payloads = append(mutation.Payloads, payload)
		mutation.Operation.Outcome = append(mutation.Operation.Outcome, scenario.Meta.RevisionID)
	}
	return nil
}

func appendBoundaryProfileMutation(
	mutation *memory.CanonicalMutation,
	request memory.CommitRequest,
	existingItems map[string]struct{},
	parentRefs map[string][]memory.RevisionRef,
	evidence map[string][]memory.EvidenceRef,
) error {
	for _, profile := range request.Profiles {
		payload, payloadRef, err := compatibilityPayloadRef("profile", profile.Meta.RevisionID, memory.CanonicalCompatibilityPayload{
			SchemaVersion:     memory.CanonicalCompatibilitySchemaVersion,
			Kind:              memory.DerivedKindProfile,
			Text:              profile.Summary,
			LegacyItemID:      profile.Meta.ItemID,
			LegacyRevisionID:  profile.Meta.RevisionID,
			LegacyOperationID: profile.Meta.OperationID,
			LegacyParents:     append([]memory.RevisionRef(nil), profile.Meta.Provenance.ParentRevisions...),
			Supersedes:        cloneOptionalRef(profile.Meta.Supersedes),
		})
		if err != nil {
			return err
		}
		ensureBoundaryItem(existingItems, &mutation.Items, request.Scope, profile.Meta.ItemID)
		mutation.Revisions = append(mutation.Revisions, memory.MemoryRevision{
			SchemaVersion: memory.MemorySchemaVersion,
			RevisionID:    profile.Meta.RevisionID,
			ItemID:        profile.Meta.ItemID,
			Revision:      profile.Meta.Revision,
			Parents:       revisionIDs(parentRefs[profile.Meta.RevisionID]),
			Temporal:      memory.Temporal{ObservedAt: profile.Meta.CreatedAt},
			Evidence:      append([]memory.EvidenceRef(nil), evidence[profile.Meta.RevisionID]...),
			Sensitivity:   memory.SensitivityStandard,
			Retention:     memory.RetentionClassStandard,
			Payload:       payloadRef,
		})
		mutation.Heads = append(mutation.Heads, memory.ItemHead{ItemID: profile.Meta.ItemID, RevisionID: profile.Meta.RevisionID})
		mutation.Payloads = append(mutation.Payloads, payload)
		mutation.Operation.Outcome = append(mutation.Operation.Outcome, profile.Meta.RevisionID)
	}
	return nil
}

func ensureBoundaryItem(existingItems map[string]struct{}, items *[]memory.MemoryItem, scope memory.Scope, itemID string) {
	if _, exists := existingItems[itemID]; exists {
		return
	}
	*items = append(*items, memory.MemoryItem{
		ItemID:    itemID,
		Scope:     scope,
		Kind:      memory.MemoryKindState,
		MemoryKey: derivedMemoryKey(itemID),
	})
	existingItems[itemID] = struct{}{}
}
