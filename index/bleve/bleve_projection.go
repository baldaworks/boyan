package bleve

import (
	"fmt"
	"strings"
	"sync"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/blevesearch/bleve/v2"
)

const (
	// BleveRecallProjectionSchema pins the application-owned projection
	// document contract independently of Bleve's internal index metadata.
	BleveRecallProjectionSchema  = "boyan-bleve/v1"
	bleveActiveGenerationFile    = ".active-generation"
	bleveScopedActivePrefix      = ".active-generation.scope."
	bleveCommittedGenerationFile = ".committed"
	bleveGenerationDirectory     = "generations"
	bleveBatchSize               = 128
)

// Config controls the disposable Bleve projection adapter.  The canonical
// store and application runtime own all semantic limits; this option only
// controls how many pending documents are flushed in one Bleve batch.
type Config struct {
	BatchSize int
}

// DefaultConfig returns the portable projection defaults.
func DefaultConfig() Config {
	return Config{BatchSize: bleveBatchSize}
}

const (
	bleveFieldTextStandard = "text_standard"
	bleveFieldTextRussian  = "text_russian"
	bleveFieldTextEnglish  = "text_english"
	bleveFieldScopeKey     = "scope_key"
	bleveFieldScopeKind    = "scope_kind"
	bleveFieldItemID       = "item_id"
	bleveFieldRevisionID   = "revision_id"
	bleveFieldRevision     = "revision"
	bleveFieldKind         = "kind"
	bleveFieldCategory     = "category"
	bleveFieldMemoryKey    = "memory_key"
	bleveFieldCreatedAt    = "created_at"
	bleveFieldSourceIDs    = "source_ids"
	bleveFieldSessionIDs   = "session_ids"
	bleveFieldChangeSeq    = "scope_change_seq"
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
	SchemaVersion  string   `json:"schema_version"`
	ScopeKey       string   `json:"scope_key"`
	ScopeKind      string   `json:"scope_kind"`
	ItemID         string   `json:"item_id"`
	RevisionID     string   `json:"revision_id"`
	Revision       string   `json:"revision"`
	Kind           string   `json:"kind"`
	Category       string   `json:"category,omitempty"`
	MemoryKey      string   `json:"memory_key,omitempty"`
	TextStandard   string   `json:"text_standard"`
	TextRussian    string   `json:"text_russian"`
	TextEnglish    string   `json:"text_english"`
	CreatedAt      string   `json:"created_at"`
	SourceIDs      []string `json:"source_ids,omitempty"`
	SessionIDs     []string `json:"session_ids,omitempty"`
	ScopeChangeSeq string   `json:"scope_change_seq"`
}

// NewBleveRecallProjection opens the active generation under root. The root
// is created if necessary; with no active marker Search safely returns no
// projection candidates so callers can use the bounded canonical fallback.
func NewBleveRecallProjection(root string) (*BleveRecallProjection, error) {
	return openWithConfig(root, DefaultConfig())
}

// Open opens a portable rebuildable lexical projection.  The directory is
// created if necessary and may be reopened by another process after Close.
func Open(root string, configs ...Config) (*BleveRecallProjection, error) {
	config := DefaultConfig()
	if len(configs) > 0 {
		config = configs[0]
	}
	if len(configs) > 1 {
		return nil, memory.PermanentError(memory.CodeInvalidDerived, "bleve projection accepts at most one config", nil)
	}
	return openWithConfig(root, config)
}

// Index is the host-neutral name for a rebuildable lexical projection.
type Index = BleveRecallProjection

func storedBleveMetadata(fields map[string]interface{}) (struct {
	Scope          memory.Scope
	ItemID         string
	RevisionID     string
	Revision       uint64
	ScopeChangeSeq uint64
}, error) {
	value := func(name string) (string, error) {
		field, ok := fields[name]
		if !ok {
			return "", memory.PermanentError(memory.CodeStoreFailure, "bleve hit metadata is incomplete", nil)
		}
		text, ok := field.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return "", memory.PermanentError(memory.CodeStoreFailure, "bleve hit metadata has an invalid value", nil)
		}
		return text, nil
	}
	scopeKey, err := value(bleveFieldScopeKey)
	if err != nil {
		return struct {
			Scope          memory.Scope
			ItemID         string
			RevisionID     string
			Revision       uint64
			ScopeChangeSeq uint64
		}{}, err
	}
	scopeKind, err := value(bleveFieldScopeKind)
	if err != nil {
		return struct {
			Scope          memory.Scope
			ItemID         string
			RevisionID     string
			Revision       uint64
			ScopeChangeSeq uint64
		}{}, err
	}
	itemID, err := value(bleveFieldItemID)
	if err != nil {
		return struct {
			Scope          memory.Scope
			ItemID         string
			RevisionID     string
			Revision       uint64
			ScopeChangeSeq uint64
		}{}, err
	}
	revisionID, err := value(bleveFieldRevisionID)
	if err != nil {
		return struct {
			Scope          memory.Scope
			ItemID         string
			RevisionID     string
			Revision       uint64
			ScopeChangeSeq uint64
		}{}, err
	}
	revisionText, err := value(bleveFieldRevision)
	if err != nil {
		return struct {
			Scope          memory.Scope
			ItemID         string
			RevisionID     string
			Revision       uint64
			ScopeChangeSeq uint64
		}{}, err
	}
	changeText, err := value(bleveFieldChangeSeq)
	if err != nil {
		return struct {
			Scope          memory.Scope
			ItemID         string
			RevisionID     string
			Revision       uint64
			ScopeChangeSeq uint64
		}{}, err
	}
	var revision, change uint64
	if _, err := fmt.Sscan(revisionText, &revision); err != nil || revision == 0 {
		return struct {
			Scope          memory.Scope
			ItemID         string
			RevisionID     string
			Revision       uint64
			ScopeChangeSeq uint64
		}{}, memory.PermanentError(memory.CodeStoreFailure, "bleve revision metadata is invalid", err)
	}
	if _, err := fmt.Sscan(changeText, &change); err != nil || change == 0 {
		return struct {
			Scope          memory.Scope
			ItemID         string
			RevisionID     string
			Revision       uint64
			ScopeChangeSeq uint64
		}{}, memory.PermanentError(memory.CodeStoreFailure, "bleve change metadata is invalid", err)
	}
	scope := memory.Scope{Key: scopeKey, Kind: memory.ScopeKind(scopeKind)}
	if err := scope.Validate(); err != nil {
		return struct {
			Scope          memory.Scope
			ItemID         string
			RevisionID     string
			Revision       uint64
			ScopeChangeSeq uint64
		}{}, err
	}
	return struct {
		Scope          memory.Scope
		ItemID         string
		RevisionID     string
		Revision       uint64
		ScopeChangeSeq uint64
	}{Scope: scope, ItemID: itemID, RevisionID: revisionID, Revision: revision, ScopeChangeSeq: change}, nil
}
