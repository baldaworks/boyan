package memory

import "time"

// LegacyDerivationRef identifies the pre-versioned derivation behavior. It is
// retained so persisted v1 inputs replay with their original operation IDs.
func LegacyDerivationRef() DerivationRef {
	return DerivationRef{
		Pipeline: "legacy-v1",
		Policy:   "legacy-v1",
		Prompt:   "legacy-v1",
		Model:    "legacy-v1",
	}
}

// SourceRef identifies one immutable raw turn source.
type SourceRef struct {
	Scope          Scope  `json:"scope"`
	ExportID       string `json:"export_id"`
	ConversationID string `json:"conversation_id"`
	SourceTurnID   string `json:"source_turn_id"`
}

// RevisionRef identifies one immutable derived revision.
type RevisionRef struct {
	ItemID     string `json:"item_id"`
	RevisionID string `json:"revision_id"`
}

// Provenance grounds a derived revision in raw sources and parent revisions.
type Provenance struct {
	RawSources      []SourceRef   `json:"raw_sources,omitempty"`
	ParentRevisions []RevisionRef `json:"parent_revisions,omitempty"`
}

// RevisionMeta contains immutable identity and lifecycle metadata.
type RevisionMeta struct {
	SchemaVersion string        `json:"schema_version"`
	Kind          DerivedKind   `json:"kind"`
	ItemID        string        `json:"item_id"`
	RevisionID    string        `json:"revision_id"`
	Revision      uint64        `json:"revision"`
	OperationID   string        `json:"operation_id"`
	Scope         Scope         `json:"scope"`
	State         RevisionState `json:"state"`
	Provenance    Provenance    `json:"provenance"`
	CreatedAt     time.Time     `json:"created_at"`
	Supersedes    *RevisionRef  `json:"supersedes,omitempty"`
}
