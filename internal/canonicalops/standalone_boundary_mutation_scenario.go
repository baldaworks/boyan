package canonicalops

import memory "github.com/baldaworks/boyan/pkg/memory"

func appendBoundaryScenarioMutation(
	mutation *memory.CanonicalMutation,
	request memory.CommitRequest,
	existingItems map[string]struct{},
	parentRefs map[string][]memory.RevisionRef,
	evidence map[string][]memory.EvidenceRef,
) error {
	for _, scenario := range request.Scenarios {
		payload, payloadRef, err := compatibilityPayloadRef(
			"scenario", scenario.Meta.RevisionID, memory.CanonicalCompatibilityPayload{
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
			},
		)
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
			Temporal:      memory.Temporal{ObservedAt: request.ProcessingAt},
			Evidence:      append([]memory.EvidenceRef(nil), evidence[scenario.Meta.RevisionID]...),
			Sensitivity:   memory.SensitivityStandard,
			Retention:     memory.RetentionClassStandard,
			Payload:       payloadRef,
		},
		)
		mutation.Heads = append(
			mutation.Heads,
			memory.ItemHead{ItemID: scenario.Meta.ItemID, RevisionID: scenario.Meta.RevisionID},
		)
		mutation.Payloads = append(mutation.Payloads, payload)
		mutation.Operation.Outcome = append(mutation.Operation.Outcome, scenario.Meta.RevisionID)
	}

	return nil
}
