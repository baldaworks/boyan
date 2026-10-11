package memory

func (s CanonicalSourceRecord) Validate() error {
	if !isCanonicalID(s.SourceID) {
		return invalidDerived("source id is required")
	}

	if err := s.Scope.Validate(); err != nil {
		return err
	}

	if err := validateSensitivityRetention(s.Sensitivity, s.Retention); err != nil {
		return err
	}

	return s.Payload.Validate()
}

func (m MessageRecord) Validate() error {
	if !isCanonicalID(m.MessageID) || !isCanonicalID(m.SourceID) {
		return invalidDerived("message identity is invalid")
	}

	if m.Role != MessageRoleUser && m.Role != MessageRoleAssistant &&
		m.Role != MessageRoleTool {
		return invalidDerived("message role is invalid")
	}

	return m.Payload.Validate()
}

func (l LifecycleEvent) Validate() error {
	if !isCanonicalID(l.EventID) || !isCanonicalID(l.RevisionID) || l.OccurredAt.IsZero() {
		return invalidDerived("lifecycle event identity is invalid")
	}

	switch l.Type {
	case LifecycleEventActivate, LifecycleEventSupersede, LifecycleEventInvalidate, LifecycleEventForget:
		return nil
	default:
		return invalidDerived("unsupported lifecycle event")
	}
}

func (p PayloadRef) Validate() error {
	if !isCanonicalID(p.ID) || !isCanonicalID(p.Digest) || p.ByteSize == 0 {
		return invalidDerived("memory payload reference is invalid")
	}

	return nil
}
