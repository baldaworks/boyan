package memory

import "time"

// MaxForgetOutcomeReferences bounds public identity lists independently of model input.
const MaxForgetOutcomeReferences = 2048

// ForgetKind identifies one independently idempotent forgetting operation.
type ForgetKind string

const (
	// ForgetKindSource forgets one immutable raw turn and its dependents.
	ForgetKindSource ForgetKind = "source"
	// ForgetKindScope forgets all readable memory in one exact locator scope.
	ForgetKindScope ForgetKind = "scope"
)

// ForgetSourceCommand requests source-aware forgetting through the canonical application service.
type ForgetSourceCommand struct {
	SchemaVersion string    `json:"schema_version"`
	Source        SourceRef `json:"source"`
	ForgottenAt   time.Time `json:"forgotten_at"`
}

// ForgetScopeCommand requests forgetting of one exact locator scope.
type ForgetScopeCommand struct {
	SchemaVersion string    `json:"schema_version"`
	Scope         Scope     `json:"scope"`
	RequestID     string    `json:"request_id"`
	ForgottenAt   time.Time `json:"forgotten_at"`
}

// ForgetOutcome is the durable, content-free canonical result of one forget.
// Persistence of this result alone does not certify public completion: the host
// also joins logical search retirement and required lexical synchronization.
// A later error may accompany this original outcome; retry preserves it.
type ForgetOutcome struct {
	SchemaVersion       string        `json:"schema_version"`
	OperationID         string        `json:"operation_id"`
	Kind                ForgetKind    `json:"kind"`
	Scope               Scope         `json:"scope"`
	ForgottenAt         time.Time     `json:"forgotten_at,omitempty"`
	SourceCount         uint64        `json:"source_count,omitempty"`
	RevisionCount       uint64        `json:"revision_count,omitempty"`
	ReferencesTruncated bool          `json:"references_truncated,omitempty"`
	ScopeVersion        uint64        `json:"scope_version"`
	Sources             []SourceRef   `json:"sources,omitempty"`
	Revisions           []RevisionRef `json:"revisions,omitempty"`
}
