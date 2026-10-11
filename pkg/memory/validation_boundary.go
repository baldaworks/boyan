package memory

// Validate verifies an idempotent conversation lifecycle export.
func (b Boundary) Validate() error {
	if b.SchemaVersion != SchemaVersionV1 {
		return PermanentError(CodePermanent, "unsupported boundary schema version", nil)
	}

	if err := b.Scope.Validate(); err != nil {
		return err
	}

	if err := b.Conversation.Validate(); err != nil {
		return err
	}

	expectedID, err := BoundaryExportID(b.Scope, b.Conversation, b.TransitionID)
	if err != nil {
		return err
	}

	if b.ExportID != expectedID {
		return PermanentError(CodePermanent, "boundary export id does not match its stable identity", nil)
	}

	switch b.Reason {
	case BoundaryReasonReset, BoundaryReasonClose, BoundaryReasonRotation, BoundaryReasonShutdown:
	default:
		return PermanentError(CodePermanent, "unsupported boundary reason", nil)
	}

	if b.OccurredAt.IsZero() {
		return PermanentError(CodePermanent, "boundary occurrence time is required", nil)
	}

	return nil
}
