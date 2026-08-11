package memory

// OperationStage identifies one independently idempotent processing stage.
type OperationStage string

const (
	// OperationStageAtoms extracts atoms from one raw turn.
	OperationStageAtoms OperationStage = "atoms"
	// OperationStageScenarios synthesizes scenarios at a conversation boundary.
	OperationStageScenarios OperationStage = "scenarios"
	// OperationStageProfile synthesizes the exact-scope profile.
	OperationStageProfile OperationStage = "profile"
)

// ProfileDisposition controls whether profile synthesis writes a revision.
type ProfileDisposition string

const (
	// ProfileDispositionSkip records a successful synthesis with no profile change.
	ProfileDispositionSkip ProfileDisposition = "skip"
	// ProfileDispositionUpsert writes a profile revision.
	ProfileDispositionUpsert ProfileDisposition = "upsert"
)
