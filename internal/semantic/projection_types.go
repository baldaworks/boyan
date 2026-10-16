// Package semantic owns Boyan's disposable, bounded vector projection.
// It deliberately stores no canonical text and is safe to delete and rebuild.
package semantic

import (
	"time"

	"github.com/baldaworks/boyan/pkg/memory"
)

const ProjectionSchemaVersion = "boyan-semantic-store/v1"

// Status describes the state advertised by an active scope pointer.
type Status string

const (
	StatusReady    Status = "ready"
	StatusDegraded Status = "degraded"
)

// View is the exact physical and model-space identity required by a reader.
type View struct {
	Scope               memory.Scope `json:"scope"`
	LexicalGenerationID string       `json:"lexical_generation_id"`
	LexicalWatermark    uint64       `json:"lexical_watermark"`
	ModelSpace          string       `json:"model_space"`
	Dimensions          int          `json:"dimensions"`
}

// Generation describes one unreachable build before publication.
type Generation struct {
	Scope               memory.Scope
	BuildID             string
	LexicalGenerationID string
	LexicalWatermark    uint64
	ModelSpace          string
	Dimensions          int
	Omitted             OmittedCounts
	ReadyAt             time.Time
}

// View returns the exact compatibility tuple carried by the generation.
func (generation Generation) View() View {
	return View{
		Scope:               generation.Scope,
		LexicalGenerationID: generation.LexicalGenerationID,
		LexicalWatermark:    generation.LexicalWatermark,
		ModelSpace:          generation.ModelSpace,
		Dimensions:          generation.Dimensions,
	}
}

// OmittedCounts records bounded, text-free coverage diagnostics.
type OmittedCounts struct {
	Ineligible uint32 `json:"ineligible,omitempty"`
	Expired    uint32 `json:"expired,omitempty"`
	Sensitive  uint32 `json:"sensitive,omitempty"`
}

// State is immutable metadata for one complete generation or degraded marker.
type State struct {
	SchemaVersion       string        `json:"schema_version"`
	Scope               memory.Scope  `json:"scope"`
	BuildID             string        `json:"build_id"`
	LexicalGenerationID string        `json:"lexical_generation_id"`
	LexicalWatermark    uint64        `json:"lexical_watermark"`
	ModelSpace          string        `json:"model_space"`
	Dimensions          int           `json:"dimensions"`
	Status              Status        `json:"status"`
	SafeReason          string        `json:"safe_reason,omitempty"`
	PublicationEpoch    uint64        `json:"publication_epoch"`
	ChunkCount          uint32        `json:"chunk_count"`
	RecordCount         uint32        `json:"record_count"`
	EncodedBytes        uint64        `json:"encoded_bytes"`
	CoverageHash        string        `json:"coverage_hash"`
	Omitted             OmittedCounts `json:"omitted"`
	ReadyAt             time.Time     `json:"ready_at"`
}

// View returns the state's exact compatibility tuple.
func (state State) View() View {
	return View{
		Scope:               state.Scope,
		LexicalGenerationID: state.LexicalGenerationID,
		LexicalWatermark:    state.LexicalWatermark,
		ModelSpace:          state.ModelSpace,
		Dimensions:          state.Dimensions,
	}
}

// CoverageRecord is the caller's complete eligible-record manifest. Stage
// rejects missing records and non-contiguous chunk ordinals against it.
type CoverageRecord struct {
	ItemID      string
	RevisionID  string
	Revision    uint64
	ContentHash string
	ChunkCount  uint32
}

// ProjectionChunk contains derived identities, filter metadata, and one unit vector.
// Source text and prepared provider input are intentionally not representable.
type ProjectionChunk struct {
	Scope               memory.Scope
	BuildID             string
	LexicalGenerationID string
	LexicalWatermark    uint64
	ModelSpace          string
	Dimensions          int
	ItemID              string
	RevisionID          string
	Revision            uint64
	Ordinal             uint32
	ContentHash         string
	InputHash           string
	ScopeChangeSeq      uint64
	Kind                memory.MemoryKind
	Category            memory.AtomCategory
	MemoryKey           memory.MemoryKey
	CreatedAt           time.Time
	// ObservedAt is optional only for legacy v1 projections. Such snapshots
	// remain readable for vector reuse but cannot certify hybrid temporal filters.
	ObservedAt      *time.Time
	ValidFrom       *time.Time
	ValidUntil      *time.Time
	ExpiresAt       *time.Time
	SourceIDs       []string
	ConversationIDs []string
	Vector          []float32
}

// Publication atomically compares and replaces one scope's active pointer.
type Publication struct {
	View

	BuildID               string
	ExpectedActiveBuildID string
	ExpectedActiveEpoch   uint64
}

// Degraded is a text-free failure marker for one exactly verified view.
type Degraded struct {
	Generation          Generation
	SafeReason          Reason
	ExpectedActiveEpoch uint64
}
