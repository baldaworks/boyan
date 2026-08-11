package memory

// Validate verifies an active source or an identity-only forgotten tombstone.
func (s SourceRecord) Validate() error {
	if s.SchemaVersion != DerivedSchemaVersionV1 {
		return invalidDerived("unsupported source record schema version")
	}
	if err := s.Ref.Validate(); err != nil {
		return err
	}
	if err := s.State.Validate(); err != nil {
		return err
	}
	switch s.State {
	case SourceStateActive:
		if s.Turn == nil {
			return invalidDerived("active source requires its raw turn")
		}
		if s.ForgottenAt != nil {
			return invalidDerived("active source cannot have a forgotten timestamp")
		}
		if err := s.Turn.Validate(); err != nil {
			return err
		}
		if sourceRefFromTurn(*s.Turn) != s.Ref {
			return invalidDerived("source reference does not match its raw turn")
		}
	case SourceStateForgotten:
		if s.Turn != nil {
			return invalidDerived("forgotten source cannot retain raw turn content")
		}
		if s.ForgottenAt == nil || s.ForgottenAt.IsZero() {
			return invalidDerived("forgotten source requires its forgotten timestamp")
		}
	}
	return nil
}

func sourceRefFromTurn(turn Turn) SourceRef {
	return SourceRef{
		Scope:          turn.Scope,
		ExportID:       turn.ExportID,
		ConversationID: turn.Conversation.ConversationID,
		SourceTurnID:   turn.SourceTurnID,
	}
}
