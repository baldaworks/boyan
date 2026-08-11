package memory

func buildReconciliation(candidate SemanticCandidate, item MemoryItem, match *MemoryItem) (Reconciliation, error) {
	revisionID := reconciliationID(
		"revision",
		item.ItemID,
		candidate.Memory.Evidence[0].SourceID,
		candidate.Memory.Evidence[0].MessageID,
		candidate.Memory.Statement,
	)
	action := ReconciliationActionCreate
	if match != nil {
		action = ReconciliationActionSupersede
	}
	lifecycle := LifecycleEvent{
		EventID:    reconciliationID("lifecycle", revisionID, string(action)),
		RevisionID: revisionID,
		OccurredAt: candidate.Memory.Temporal.ObservedAt,
	}
	if action == ReconciliationActionCreate {
		lifecycle.Type = LifecycleEventActivate
	} else {
		lifecycle.Type = LifecycleEventSupersede
	}
	if err := lifecycle.Validate(); err != nil {
		return Reconciliation{}, err
	}
	reconciliation := Reconciliation{
		Action:     action,
		Item:       item,
		RevisionID: revisionID,
		Provenance: append([]EvidenceRef(nil), candidate.Memory.Evidence...),
		Lifecycle:  lifecycle,
	}
	if match != nil {
		reconciliation.Supersedes = match
	}
	return reconciliation, nil
}
