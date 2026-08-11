package memory

// CanonicalCompatibilitySchemaVersion identifies the small rebuildable
// metadata envelope used when canonical records must satisfy the existing
// derived search/trace presentation contract. It is projection metadata, not
// a second source of truth.
const CanonicalCompatibilitySchemaVersion = "boyan-canonical-compatibility/v1"

// CanonicalCompatibilityPayload is the rebuildable legacy-presentation
// metadata attached to a canonical payload.
type CanonicalCompatibilityPayload struct {
	SchemaVersion     string        `json:"schema_version"`
	Kind              DerivedKind   `json:"kind"`
	Category          *AtomCategory `json:"category,omitempty"`
	TopicKey          string        `json:"topic_key,omitempty"`
	Title             string        `json:"title,omitempty"`
	Text              string        `json:"text"`
	LegacyItemID      string        `json:"legacy_item_id"`
	LegacyRevisionID  string        `json:"legacy_revision_id"`
	LegacyOperationID string        `json:"legacy_operation_id"`
	LegacyParents     []RevisionRef `json:"legacy_parents,omitempty"`
	Supersedes        *RevisionRef  `json:"supersedes,omitempty"`
}
