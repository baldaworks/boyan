package memory

import "time"

// RecallRecord is one canonical record hydrated for recall.
type RecallRecord struct {
	Scope             Scope
	ItemID            string
	RevisionID        string
	Revision          uint64
	Kind              MemoryKind
	Category          *AtomCategory
	LegacyKind        *DerivedKind
	LegacyItemID      string
	LegacyRevisionID  string
	LegacyOperationID string
	LegacySupersedes  *RevisionRef
	LegacyParents     []RevisionRef
	TopicKey          string
	Title             string
	MemoryKey         MemoryKey
	Text              string
	State             RevisionState
	CreatedAt         time.Time
	Temporal          Temporal
	Sensitivity       Sensitivity
	Retention         RetentionClass
	Evidence          []EvidenceRef
	SourceIDs         []string
	SourceRefs        []SourceRef
	ConversationIDs   []string
	ScopeChangeSeq    uint64
}
