package semantic

import (
	"context"

	"github.com/baldaworks/boyan/pkg/memory"
)

// ScanCounts contains text-free diagnostics for the bounded semantic scan.
type ScanCounts struct {
	ScannedChunks int
}

// SearchSnapshot pins both physical channels to one immutable view. Searches
// honor context cancellation and return identities, scores and counts only.
// The caller must Close on every path, including certification failure.
type SearchSnapshot interface {
	LexicalManifest() memory.ProjectionManifest
	SemanticState() (State, bool, error)
	SearchLexical(ctx context.Context, request memory.RecallRequest, limit int) ([]memory.RecallProjectionHit, error)
	SearchSemantic(ctx context.Context, request memory.RecallRequest, queries [][]float32,
		limit int) ([]DenseMatch, ScanCounts, error)
	Close() error
}

// ReusableChunk carries only the identity needed to validate build-time reuse.
// Providers and recall callers never receive this build-only vector material.
type ReusableChunk struct {
	ItemID      string
	RevisionID  string
	Revision    uint64
	ContentHash string
	InputHash   string
	Ordinal     uint32
	Vector      []float32
}

// Reusable is an owned, validated build-only copy, bounded by the same logical
// 8192-chunk/64MiB admission contract as the published generation.
type Reusable struct {
	State  State
	Chunks []ReusableChunk
}
