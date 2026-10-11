package memory

func (t Turn) validateTurnEnvelope() (TurnTerminalStatus, error) {
	if t.SchemaVersion != SchemaVersionV1 {
		return "", PermanentError(CodePermanent, "unsupported turn schema version", nil)
	}

	if err := t.Scope.Validate(); err != nil {
		return "", err
	}

	if err := t.Conversation.Validate(); err != nil {
		return "", err
	}

	expectedID, err := TurnExportID(t.Scope, t.Conversation, t.SourceTurnID)
	if err != nil {
		return "", err
	}

	if t.ExportID != expectedID {
		return "", PermanentError(CodePermanent, "turn export id does not match its stable identity", nil)
	}

	if t.SourceID != "" && t.SourceID != TurnSourceID(t.ExportID) {
		return "", PermanentError(CodePermanent, "turn source id does not match turn identity", nil)
	}

	if t.CompletedAt.IsZero() {
		return "", PermanentError(CodePermanent, "turn completion time is required", nil)
	}

	return normalizeTurnTerminalStatus(t.TerminalStatus)
}

func normalizeTurnTerminalStatus(status TurnTerminalStatus) (TurnTerminalStatus, error) {
	if status == "" {
		status = TurnTerminalStatusSuccess
	}

	switch status {
	case TurnTerminalStatusSuccess, TurnTerminalStatusFailed, TurnTerminalStatusInterrupted:
		return status, nil
	default:
		return "", PermanentError(CodePermanent, "turn terminal status is invalid", nil)
	}
}
