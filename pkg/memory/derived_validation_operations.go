package memory

// Validate verifies a supported idempotent processing stage.
func (s OperationStage) Validate() error {
	switch s {
	case OperationStageAtoms, OperationStageScenarios, OperationStageProfile:
		return nil
	default:
		return invalidDerived("unsupported operation stage")
	}
}

// Validate verifies a supported profile synthesis disposition.
func (d ProfileDisposition) Validate() error {
	switch d {
	case ProfileDispositionSkip, ProfileDispositionUpsert:
		return nil
	default:
		return invalidDerived("unsupported profile candidate disposition")
	}
}
