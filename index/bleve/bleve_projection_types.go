package bleve

import (
	"sync"

	"github.com/blevesearch/bleve/v2"
)

// BleveRecallProjection is a rebuildable persistent lexical adapter. It
// stores only disposable projection documents; RecallService hydrates all
// returned IDs from canonical storage before exposing text.
type BleveRecallProjection struct {
	mu             sync.RWMutex
	root           string
	batchSize      int
	activeID       string
	active         bleve.Index
	activeByScope  map[string]bleve.Index
	activeScopeIDs map[string]string
}

// BleveGeneration is a write-once build generation. A generation must be
// committed and closed before it can be activated.
type BleveGeneration struct {
	mu        sync.Mutex
	id        string
	path      string
	batchSize int
	index     bleve.Index
	batch     *bleve.Batch
	closed    bool
	committed bool
}

type bleveRecallDocument struct {
	SchemaVersion   string   `json:"schema_version"`
	ScopeKey        string   `json:"scope_key"`
	ScopeKind       string   `json:"scope_kind"`
	ItemID          string   `json:"item_id"`
	RevisionID      string   `json:"revision_id"`
	Revision        string   `json:"revision"`
	Kind            string   `json:"kind"`
	Category        string   `json:"category,omitempty"`
	MemoryKey       string   `json:"memory_key,omitempty"`
	TextStandard    string   `json:"text_standard"`
	TextRussian     string   `json:"text_russian"`
	TextEnglish     string   `json:"text_english"`
	CreatedAt       string   `json:"created_at"`
	SourceIDs       []string `json:"source_ids,omitempty"`
	ConversationIDs []string `json:"conversation_ids,omitempty"`
	ScopeChangeSeq  string   `json:"scope_change_seq"`
}

// Index is the host-neutral name for a rebuildable lexical projection.
type Index = BleveRecallProjection
