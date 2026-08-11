package memory

// MemoryCandidate is untrusted derivation output. It deliberately has no
// persistent item, revision, scope, or lifecycle identifiers.
type MemoryCandidate struct {
	Kind        MemoryKind     `json:"kind"`
	Statement   string         `json:"statement"`
	Temporal    Temporal       `json:"temporal"`
	Evidence    []EvidenceRef  `json:"evidence"`
	Sensitivity Sensitivity    `json:"sensitivity"`
	Retention   RetentionClass `json:"retention"`
}

// MemoryItem is an application-owned logical identity. Callers never supply its ID.
type MemoryItem struct {
	ItemID    string     `json:"item_id"`
	Scope     Scope      `json:"scope"`
	Kind      MemoryKind `json:"kind"`
	MemoryKey MemoryKey  `json:"memory_key,omitempty"`
}

// MemoryRevision is immutable canonical metadata for one memory change.
type MemoryRevision struct {
	SchemaVersion string         `json:"schema_version"`
	RevisionID    string         `json:"revision_id"`
	ItemID        string         `json:"item_id"`
	Revision      uint64         `json:"revision"`
	Parents       []string       `json:"parents,omitempty"`
	Temporal      Temporal       `json:"temporal"`
	Evidence      []EvidenceRef  `json:"evidence"`
	Sensitivity   Sensitivity    `json:"sensitivity"`
	Retention     RetentionClass `json:"retention"`
	Payload       PayloadRef     `json:"payload"`
}
