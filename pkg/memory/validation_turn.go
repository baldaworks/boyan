package memory

// Validate verifies a terminal, text-only, idempotent turn export.
func (t Turn) Validate() error {
	status, err := t.validateTurnEnvelope()
	if err != nil {
		return err
	}
	if len(t.Messages) == 0 {
		return PermanentError(CodePermanent, "turn requires a user message", nil)
	}
	if err := t.validateTurnLead(status); err != nil {
		return err
	}
	return t.validateTurnTools()
}
