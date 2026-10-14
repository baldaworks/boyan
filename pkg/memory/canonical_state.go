package memory

import "time"

// MaxCanonicalReadRecords bounds one canonical read batch.
const MaxCanonicalReadRecords = 256

// ScopeState is the small mutable record for one exact memory scope.
// All memory content is stored in immutable records referenced by this state.
type ScopeState struct {
	SchemaVersion string `json:"schema_version"`
	Scope         Scope  `json:"scope"`
	Version       uint64 `json:"version"`
	ChangeSeq     uint64 `json:"change_seq"`
}

// ScopeChange is an ordered, immutable projection-replay entry.
type ScopeChange struct {
	Sequence     uint64    `json:"sequence"`
	ScopeVersion uint64    `json:"scope_version,omitempty"`
	OperationID  string    `json:"operation_id"`
	OccurredAt   time.Time `json:"occurred_at"`
	RevisionIDs  []string  `json:"revision_ids,omitempty"`
	// RequiresRebuild marks changes whose complete target references exceed the
	// bounded delta representation. Such entries must never advance via Apply.
	RequiresRebuild bool `json:"requires_rebuild,omitempty"`
}

// Validate verifies one exact-scope canonical state header.
func (s ScopeState) Validate() error {
	if s.SchemaVersion != CanonicalSchemaVersionV1 {
		return invalidDerived("canonical scope state schema version is invalid")
	}

	return s.Scope.Validate()
}

// Validate verifies one immutable scope-change replay record.
func (c ScopeChange) Validate() error {
	if c.Sequence == 0 || !isCanonicalID(c.OperationID) || c.OccurredAt.IsZero() {
		return invalidDerived("scope change is invalid")
	}

	for _, revisionID := range c.RevisionIDs {
		if !isCanonicalID(revisionID) {
			return invalidDerived("scope change revision is invalid")
		}
	}

	return nil
}
