package memory

func (t Turn) validateTurnLead(status TurnTerminalStatus) error {
	if err := validateMessage(t.Messages[0], t.ExportID, MessageRoleUser); err != nil {
		return err
	}
	if len(t.Messages) > 1 && t.Messages[1].Role == MessageRoleAssistant {
		return validateMessage(t.Messages[1], t.ExportID, MessageRoleAssistant)
	}
	if status == TurnTerminalStatusSuccess {
		return PermanentError(CodePermanent, "successful turn requires assistant text", nil)
	}
	return nil
}

func (t Turn) validateTurnTools() error {
	next := 1
	if len(t.Messages) > 1 && t.Messages[1].Role == MessageRoleAssistant {
		next = 2
	}
	if len(t.Messages)-next > maxTurnToolMessages {
		return PermanentError(CodePermanent, "turn tool message count exceeds the limit", nil)
	}
	seenTools := make(map[string]struct{}, len(t.Messages)-next)
	for ; next < len(t.Messages); next++ {
		message := t.Messages[next]
		if err := validateToolMessage(message, t.ExportID); err != nil {
			return err
		}
		if _, exists := seenTools[message.MessageID]; exists {
			return PermanentError(CodePermanent, "turn tool message identity is duplicated", nil)
		}
		seenTools[message.MessageID] = struct{}{}
	}
	return nil
}
