package memory

import (
	"context"
	"time"
)

// OperationLookup asks whether one exact-scope stage already committed.
type OperationLookup struct {
	SchemaVersion string         `json:"schema_version"`
	OperationID   string         `json:"operation_id"`
	Stage         OperationStage `json:"stage"`
	Scope         Scope          `json:"scope"`
}

// OperationLookupResult contains an optional prior committed outcome.
type OperationLookupResult struct {
	Found   bool             `json:"found"`
	Outcome OperationOutcome `json:"outcome,omitempty"`
}

// RevisionTransition atomically changes recall state for one prior revision.
type RevisionTransition struct {
	Ref  RevisionRef   `json:"ref"`
	From RevisionState `json:"from"`
	To   RevisionState `json:"to"`
}

// CommitRequest atomically applies one idempotent exact-scope operation.
type CommitRequest struct {
	BoundaryIdentity *CanonicalBoundaryIdentityCommit `json:"-"`
	// ProcessingAt is the captured current instant for one boundary attempt.
	ProcessingAt         time.Time            `json:"-"`
	SchemaVersion        string               `json:"schema_version"`
	OperationID          string               `json:"operation_id"`
	Stage                OperationStage       `json:"stage"`
	Scope                Scope                `json:"scope"`
	ExpectedScopeVersion uint64               `json:"expected_scope_version"`
	Sources              []SourceRecord       `json:"sources,omitempty"`
	Atoms                []Atom               `json:"atoms,omitempty"`
	Scenarios            []Scenario           `json:"scenarios,omitempty"`
	Profiles             []Profile            `json:"profiles,omitempty"`
	Transitions          []RevisionTransition `json:"transitions,omitempty"`
}

// ScopeChangeSequenceReader is an optional consistency port for providers
// that can expose the canonical change watermark.
type ScopeChangeSequenceReader interface {
	CurrentScopeChangeSeq(ctx context.Context, scope Scope) (uint64, error)
}
