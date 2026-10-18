package canonicalsql

import memory "github.com/baldaworks/boyan/pkg/memory"

func populateRecallRecordSources(scope memory.Scope, view canonicalRecallView, record *memory.RecallRecord) error {
	for _, source := range view.sourceRefs {
		if source.Scope != scope {
			return memory.PermanentError(memory.CodeScopeViolation, "canonical recall source scope does not match", nil)
		}
	}
	// Canonical source IDs are the stable source identities used in EvidenceRef.
	// Keep them as the public source filter keys and retain the conversation IDs
	// separately for conversation filtering.
	record.SourceIDs = make([]string, 0, len(view.revision.Evidence))
	for _, evidence := range view.revision.Evidence {
		record.SourceIDs = appendUnique(record.SourceIDs, evidence.SourceID)
	}

	record.SourceRefs = append([]memory.SourceRef(nil), view.sourceRefs...)
	record.ConversationIDs = append([]string(nil), view.conversations...)

	return nil
}
