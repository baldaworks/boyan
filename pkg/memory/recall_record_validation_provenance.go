package memory

func (r RecallRecord) validateRecallProvenance() error {
	for _, sourceID := range r.SourceIDs {
		if !isCanonicalID(sourceID) {
			return invalidDerived("recall source identity is invalid")
		}
	}
	for _, source := range r.SourceRefs {
		if err := source.Validate(); err != nil {
			return err
		}
		if source.Scope != r.Scope {
			return PermanentError(CodeScopeViolation, "recall source reference scope does not match", nil)
		}
	}
	for _, conversationID := range r.ConversationIDs {
		if !isCanonicalID(conversationID) {
			return invalidDerived("recall conversation identity is invalid")
		}
	}
	if len(r.SourceIDs) == 0 || len(r.Evidence) == 0 {
		return invalidDerived("recall record provenance is required")
	}
	if len(r.Evidence) > MaxRecallEvidence {
		return limitExceeded("recall evidence exceeds the compact bound")
	}
	for _, evidence := range r.Evidence {
		if err := evidence.Validate(); err != nil {
			return err
		}
		if evidence.SourceID == "" || !containsString(r.SourceIDs, evidence.SourceID) {
			return PermanentError(CodeScopeViolation, "recall evidence is not grounded in the record", nil)
		}
	}
	return nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
