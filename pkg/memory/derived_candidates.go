package memory

// AtomCandidate is untrusted model output for one proposed atom.
type AtomCandidate struct {
	Category AtomCategory      `json:"category"`
	Text     string            `json:"text"`
	Relation CandidateRelation `json:"relation"`
	Target   *RevisionRef      `json:"target,omitempty"`
}

// ScenarioCandidate is untrusted model output for one proposed scenario.
type ScenarioCandidate struct {
	TopicKey   string        `json:"topic_key"`
	Title      string        `json:"title"`
	Summary    string        `json:"summary"`
	Atoms      []RevisionRef `json:"atoms"`
	Supersedes *RevisionRef  `json:"supersedes,omitempty"`
}

// ProfileCandidate is untrusted model output for one proposed profile.
type ProfileCandidate struct {
	Disposition ProfileDisposition `json:"disposition"`
	Summary     string             `json:"summary"`
	Atoms       []RevisionRef      `json:"atoms,omitempty"`
	Scenarios   []RevisionRef      `json:"scenarios,omitempty"`
	Supersedes  *RevisionRef       `json:"supersedes,omitempty"`
}

// OperationOutcome is the durable result of one idempotent processing stage.
type OperationOutcome struct {
	SchemaVersion string         `json:"schema_version"`
	OperationID   string         `json:"operation_id"`
	Stage         OperationStage `json:"stage"`
	Scope         Scope          `json:"scope"`
	ScopeVersion  uint64         `json:"scope_version"`
	Revisions     []RevisionRef  `json:"revisions,omitempty"`
}

// BoundaryOutcome contains the independently durable scenario and profile stages.
// When profile synthesis fails, Scenarios remains populated for safe replay.
type BoundaryOutcome struct {
	SchemaVersion string           `json:"schema_version"`
	Scope         Scope            `json:"scope"`
	Scenarios     OperationOutcome `json:"scenarios"`
	Profile       OperationOutcome `json:"profile"`
}
