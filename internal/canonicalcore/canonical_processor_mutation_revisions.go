package canonicalcore

import . "github.com/baldaworks/boyan/pkg/memory"

func (p *CanonicalTurnProcessor) appendCanonicalTurnRevisions(
	mutation *CanonicalMutation,
	turn Turn,
	active []ActiveCanonicalMemory,
	activeView []ActiveMemoryItem,
	sourceID string,
	messageByID map[string]Message,
	candidates []SemanticCandidate,
) error {
	seenItems := make(map[string]struct{}, len(candidates))

	seenRevisions := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		canonicalCandidate, err := canonicalizeCandidate(sourceID, messageByID, candidate)
		if err != nil {
			return err
		}

		reconciliation, err := p.policy.ReconcileActive(turn.Scope, canonicalCandidate, activeView)
		if err != nil {
			return err
		}

		if _, exists := seenItems[reconciliation.Item.ItemID]; exists {
			return invalidDerived("canonical semantic candidates resolve to one item more than once")
		}

		if _, exists := seenRevisions[reconciliation.RevisionID]; exists {
			return invalidDerived("canonical semantic candidates contain duplicate revisions")
		}

		seenItems[reconciliation.Item.ItemID] = struct{}{}
		seenRevisions[reconciliation.RevisionID] = struct{}{}

		if err := p.appendReconciledRevision(mutation, active, canonicalCandidate, reconciliation); err != nil {
			return err
		}
	}

	return nil
}

func (p *CanonicalTurnProcessor) appendReconciledRevision(
	mutation *CanonicalMutation,
	active []ActiveCanonicalMemory,
	candidate SemanticCandidate,
	reconciliation Reconciliation,
) error {
	parentRevision, revisionNumber, err := activeRevision(active, reconciliation.Supersedes)
	if err != nil {
		return err
	}

	revisionPayload, revisionRef, err := p.payload(
		canonicalScopedPayloadID(mutation.Scope, "revision", reconciliation.RevisionID),
		[]byte(candidate.Memory.Statement),
	)
	if err != nil {
		return err
	}

	revision := MemoryRevision{
		SchemaVersion: MemorySchemaVersion,
		RevisionID:    reconciliation.RevisionID,
		ItemID:        reconciliation.Item.ItemID,
		Revision:      revisionNumber,
		Temporal:      candidate.Memory.Temporal,
		Evidence:      append([]EvidenceRef(nil), reconciliation.Provenance...),
		Sensitivity:   candidate.Memory.Sensitivity,
		Retention:     candidate.Memory.Retention,
		Payload:       revisionRef,
	}
	if parentRevision != nil {
		revision.Parents = []string{parentRevision.RevisionID}
	}

	if reconciliation.Action == ReconciliationActionCreate {
		mutation.Items = append(mutation.Items, reconciliation.Item)
	}

	mutation.Revisions = append(mutation.Revisions, revision)
	mutation.Lifecycle = append(mutation.Lifecycle, reconciliation.Lifecycle)
	mutation.Heads = append(mutation.Heads, ItemHead{
		ItemID:     reconciliation.Item.ItemID,
		RevisionID: reconciliation.RevisionID,
	})
	mutation.Payloads = append(mutation.Payloads, revisionPayload)
	mutation.Operation.Outcome = append(mutation.Operation.Outcome, reconciliation.RevisionID)

	return nil
}
