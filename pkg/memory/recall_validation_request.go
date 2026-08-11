package memory

import "strings"

// Validate verifies a bounded exact-scope canonical recall query.
func (r RecallRequest) Validate() error {
	if r.SchemaVersion != RecallSchemaVersionV1 {
		return PermanentError(CodeInvalidQuery, "unsupported recall schema version", nil)
	}
	if err := r.Scope.Validate(); err != nil {
		return err
	}
	if r.Query == "" || strings.TrimSpace(r.Query) != r.Query || len(r.Query) > MaxSearchQueryBytes {
		return PermanentError(CodeInvalidQuery, "recall query is invalid", nil)
	}
	if r.Limit < 1 || r.Limit > MaxSearchLimit {
		return PermanentError(CodeInvalidQuery, "recall limit is outside the allowed range", nil)
	}
	if r.Kind != nil {
		switch *r.Kind {
		case MemoryKindState, MemoryKindEvent:
		default:
			return PermanentError(CodeInvalidQuery, "recall kind is invalid", nil)
		}
	}
	if r.Category != nil {
		if err := r.Category.Validate(); err != nil {
			return PermanentError(CodeInvalidQuery, "recall category is invalid", nil)
		}
	}
	for name, value := range map[string]string{"source": r.SourceID, "conversation": r.ConversationID, "memory key": string(r.MemoryKey)} {
		if value != "" && !isCanonicalID(value) {
			return PermanentError(CodeInvalidQuery, name+" filter is invalid", nil)
		}
	}
	if r.AsOf != nil && r.AsOf.IsZero() {
		return PermanentError(CodeInvalidQuery, "recall as_of is invalid", nil)
	}
	return nil
}
