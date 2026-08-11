package memory

// Validate verifies a supported derived revision state.
func (s RevisionState) Validate() error {
	switch s {
	case RevisionStateActive, RevisionStateSuperseded, RevisionStateInvalidated:
		return nil
	default:
		return invalidDerived("unsupported revision state")
	}
}

// Validate verifies a supported raw source state.
func (s SourceState) Validate() error {
	switch s {
	case SourceStateActive, SourceStateForgotten:
		return nil
	default:
		return invalidDerived("unsupported source state")
	}
}
