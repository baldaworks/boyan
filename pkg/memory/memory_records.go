package memory

const (
	// MemorySchemaVersion identifies the portable immutable-memory contract.
	MemorySchemaVersion = "boyan-memory/v1"
	maxMemoryTextBytes  = 32 * 1024
)

// MemoryKind distinguishes stable state from a distinct episodic event.
type MemoryKind string

const (
	MemoryKindState MemoryKind = "state"
	MemoryKindEvent MemoryKind = "event"
)

// MemoryKey is an application-derived stable semantic identity for state memory.
type MemoryKey string

// AssertionMode identifies the evidence authority used for a memory revision.
type AssertionMode string

const (
	AssertionModeUser        AssertionMode = "user"
	AssertionModeTrustedTool AssertionMode = "trusted_tool"
)

// Sensitivity controls whether data is eligible for durable retention.
type Sensitivity string

const (
	SensitivityStandard  Sensitivity = "standard"
	SensitivitySensitive Sensitivity = "sensitive"
)

// RetentionClass controls lifecycle handling for stored payloads.
type RetentionClass string

const (
	RetentionClassStandard  RetentionClass = "standard"
	RetentionClassEphemeral RetentionClass = "ephemeral"
)
