package canonicalsql

import (
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func recordMatchesDerivedSearchFilters(
	record memory.RecallRecord,
	normalized memory.DerivedSearchRequest,
) (bool, error) {
	if !strings.Contains(strings.ToLower(record.Text), strings.ToLower(normalized.Query)) {
		return false, nil
	}

	if normalized.AsOf != nil && record.CreatedAt.After(normalized.AsOf.UTC()) {
		return false, nil
	}

	if !recordMatchesDerivedIdentity(record, normalized) {
		return false, nil
	}

	if normalized.MinScopeChangeSeq != 0 &&
		record.ScopeChangeSeq < normalized.MinScopeChangeSeq {
		return false, memory.RetryableError(memory.CodeConflict, "canonical search watermark has not caught up", nil)
	}

	return true, nil
}

func canonicalRecordMatchesSource(record memory.RecallRecord, sourceID string) bool {
	if containsString(record.SourceIDs, sourceID) {
		return true
	}

	for _, source := range record.SourceRefs {
		if source.ExportID == sourceID {
			return true
		}
	}

	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}

	return false
}

func recordMatchesDerivedIdentity(record memory.RecallRecord, normalized memory.DerivedSearchRequest) bool {
	if normalized.MemoryKey != "" &&
		record.MemoryKey != memory.MemoryKey(normalized.MemoryKey) {
		return false
	}

	if normalized.SourceID != "" &&
		!canonicalRecordMatchesSource(record, normalized.SourceID) {
		return false
	}

	if normalized.ConversationID != "" &&
		!containsString(record.ConversationIDs, normalized.ConversationID) {
		return false
	}

	return true
}
