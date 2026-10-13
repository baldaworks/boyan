package memory

import "time"

// DerivedSearchRequest asks for bounded memory in one exact scope.
type DerivedSearchRequest struct {
	SchemaVersion     string        `json:"schema_version"`
	Scope             Scope         `json:"scope"`
	Query             string        `json:"query"`
	Kind              *DerivedKind  `json:"kind,omitempty"`
	Category          *AtomCategory `json:"category,omitempty"`
	Limit             int           `json:"limit"`
	AsOf              *time.Time    `json:"as_of,omitempty"`
	SourceID          string        `json:"source_id,omitempty"`
	ConversationID    string        `json:"conversation_id,omitempty"`
	MemoryKey         string        `json:"memory_key,omitempty"`
	MinScopeChangeSeq uint64        `json:"min_scope_change_seq,omitempty"`
}

// SearchHit is one full Store-owned revision plus an optional relevance score.
type SearchHit struct {
	Canonical *CanonicalTraceRevision `json:"canonical,omitempty"`
	Atom      *Atom                   `json:"atom,omitempty"`
	Scenario  *Scenario               `json:"scenario,omitempty"`
	Profile   *Profile                `json:"profile,omitempty"`
	Score     *float64                `json:"score,omitempty"`
}

// DerivedReference is one structured untrusted recall result.
type DerivedReference struct {
	SchemaVersion string         `json:"schema_version"`
	Trust         ReferenceTrust `json:"trust"`
	Kind          DerivedKind    `json:"kind"`
	Scope         Scope          `json:"scope"`
	ItemID        string         `json:"item_id"`
	RevisionID    string         `json:"revision_id"`
	Revision      uint64         `json:"revision"`
	State         RevisionState  `json:"state"`
	Category      *AtomCategory  `json:"category,omitempty"`
	TopicKey      string         `json:"topic_key,omitempty"`
	Title         string         `json:"title,omitempty"`
	Text          string         `json:"text"`
	CreatedAt     time.Time      `json:"created_at"`
	Score         *float64       `json:"score,omitempty"`
	Provenance    Provenance     `json:"provenance"`
}

// DerivedSearchResponse contains bounded untrusted references for one scope.
type DerivedSearchResponse struct {
	SchemaVersion string             `json:"schema_version"`
	Trust         ReferenceTrust     `json:"trust"`
	Scope         Scope              `json:"scope"`
	Results       []DerivedReference `json:"results"`
}
