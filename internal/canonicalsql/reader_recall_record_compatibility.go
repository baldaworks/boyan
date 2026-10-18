package canonicalsql

import memory "github.com/baldaworks/boyan/pkg/memory"

func buildRecallRecordCompatibility(
	scope memory.Scope,
	kind memory.MemoryKind,
	text []byte,
) recallRecordCompatibility {
	compat := recallRecordCompatibility{
		text:     string(text),
		category: recallCategory(kind),
	}

	payload, ok := canonicalCompatibilityPayload(text, scope)
	if !ok {
		return compat
	}

	compat.text = payload.Text
	legacyKind := payload.Kind
	compat.legacyKind = &legacyKind
	compat.legacyItemID = payload.LegacyItemID
	compat.legacyRevisionID = payload.LegacyRevisionID

	compat.legacyOperationID = payload.LegacyOperationID
	if payload.Supersedes != nil {
		copyOf := *payload.Supersedes
		compat.legacySupersedes = &copyOf
	}

	compat.legacyParents = append([]memory.RevisionRef(nil), payload.LegacyParents...)
	if payload.Category != nil {
		compat.category = *payload.Category
	}

	compat.topicKey = payload.TopicKey
	compat.title = payload.Title

	return compat
}
