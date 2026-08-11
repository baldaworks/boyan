package memory

import "strings"

func (r RecallRecord) validateRecallIdentity(request RecallRequest) error {
	if r.Scope != request.Scope {
		return PermanentError(CodeScopeViolation, "recall record scope does not match request", nil)
	}
	if !isCanonicalID(r.ItemID) || !isCanonicalID(r.RevisionID) || r.Revision == 0 || r.State != RevisionStateActive {
		return PermanentError(CodeForgotten, "recall record is not active", nil)
	}
	switch r.Kind {
	case MemoryKindState, MemoryKindEvent:
	default:
		return invalidDerived("recall record kind is invalid")
	}
	if r.Category != nil {
		if err := r.Category.Validate(); err != nil {
			return err
		}
	}
	if strings.TrimSpace(r.Text) == "" || len(r.Text) > MaxDerivedTextBytes || r.CreatedAt.IsZero() {
		return invalidDerived("recall record text or timestamp is invalid")
	}
	if err := r.Temporal.Validate(); err != nil {
		return err
	}
	if r.Sensitivity != SensitivityStandard {
		return PermanentError(CodeForgotten, "recall record sensitivity is not allowed", nil)
	}
	if r.Retention != RetentionClassStandard && r.Retention != RetentionClassEphemeral {
		return PermanentError(CodeForgotten, "recall record retention is not allowed", nil)
	}
	if r.ScopeChangeSeq == 0 || r.ScopeChangeSeq < request.MinScopeChangeSeq {
		return PermanentError(CodeConflict, "recall record is behind the requested scope consistency", nil)
	}
	return nil
}
