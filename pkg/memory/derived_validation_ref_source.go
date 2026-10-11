package memory

// Validate verifies a complete exact-scope raw source identity.
func (r SourceRef) Validate() error {
	if err := r.Scope.Validate(); err != nil {
		return err
	}

	if !isCanonicalID(r.ExportID) {
		return invalidDerived("source export id is required")
	}

	if !isCanonicalID(r.ConversationID) {
		return invalidDerived("source conversation id is required")
	}

	if !isCanonicalID(r.SourceTurnID) {
		return invalidDerived("source turn id is required")
	}

	return nil
}
