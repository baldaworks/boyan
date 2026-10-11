package canonicalops

import memory "github.com/baldaworks/boyan/pkg/memory"

func appendBoundaryProfileMutation(
	mutation *memory.CanonicalMutation,
	request memory.CommitRequest,
	existingItems map[string]struct{},
	parentRefs map[string][]memory.RevisionRef,
	evidence map[string][]memory.EvidenceRef,
) error {
	for _, profile := range request.Profiles {
		payload, payloadRef, err := compatibilityPayloadRef(
			"profile", profile.Meta.RevisionID, memory.CanonicalCompatibilityPayload{
				SchemaVersion:     memory.CanonicalCompatibilitySchemaVersion,
				Kind:              memory.DerivedKindProfile,
				Text:              profile.Summary,
				LegacyItemID:      profile.Meta.ItemID,
				LegacyRevisionID:  profile.Meta.RevisionID,
				LegacyOperationID: profile.Meta.OperationID,
				LegacyParents:     append([]memory.RevisionRef(nil), profile.Meta.Provenance.ParentRevisions...),
				Supersedes:        cloneOptionalRef(profile.Meta.Supersedes),
			},
		)
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
		},
		)
		mutation.Heads = append(
			mutation.Heads,
			memory.ItemHead{ItemID: profile.Meta.ItemID, RevisionID: profile.Meta.RevisionID},
		)
		mutation.Payloads = append(mutation.Payloads, payload)
		mutation.Operation.Outcome = append(mutation.Operation.Outcome, profile.Meta.RevisionID)
	}

	return nil
}
