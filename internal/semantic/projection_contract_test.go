package semantic_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	semantic "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

//nolint:cyclop,funlen // One fixed fixture guards admission, v1 encoding, and ownership together.
func TestPortableProjectionPreflightBoundsAndOwnership(t *testing.T) {
	t.Parallel()

	generation := semantic.Generation{Scope: memory.Scope{Key: "test:scope", Kind: memory.ScopeKindPersonal},
		BuildID: "build", LexicalGenerationID: "lexical", LexicalWatermark: 7,
		ModelSpace: strings.Repeat("1", 64), Dimensions: 2, ReadyAt: time.Date(2026, 10, 15, 1, 2, 3, 0, time.UTC)}
	coverage := []semantic.CoverageRecord{{ItemID: "item", RevisionID: "revision", Revision: 1,
		ContentHash: strings.Repeat("2", 64), ChunkCount: 1}}
	chunk := semantic.ProjectionChunk{Scope: generation.Scope, BuildID: generation.BuildID,
		LexicalGenerationID: generation.LexicalGenerationID, LexicalWatermark: 7,
		ModelSpace: generation.ModelSpace, Dimensions: 2, ItemID: "item", RevisionID: "revision", Revision: 1,
		ContentHash: coverage[0].ContentHash, InputHash: strings.Repeat("3", 64), ScopeChangeSeq: 7,
		Kind: memory.MemoryKindState, CreatedAt: generation.ReadyAt, SourceIDs: []string{"z", "a"}}
	chunks := []semantic.ProjectionChunk{chunk}

	state, err := semantic.PreflightStage(context.Background(), generation, coverage, chunks)
	if err != nil || state.ChunkCount != 1 || state.RecordCount != 1 {
		t.Fatalf("PreflightStage() = (%#v, %v)", state, err)
	}
	// The v1 fixture includes 71 logical row-key bytes and 67 state-key bytes;
	// backend file, index, and WAL sizes must never enter this admission budget.
	const wantCoverage = "50ad373cfb62298100ae7f8afd320926d9bbe14b1b380b7731ea3fef7d6e6e47"

	if state.EncodedBytes != 1304 || state.CoverageHash != wantCoverage {
		t.Fatalf("v1 encoding/hash drift: %#v", state)
	}

	if chunks[0].Vector != nil || chunks[0].SourceIDs[0] != "z" {
		t.Fatal("preflight mutated caller-owned vectors or metadata")
	}

	chunks[0].Vector = []float32{1, 0}
	observed := generation.ReadyAt
	chunks[0].ObservedAt = &observed

	_, ordered, err := semantic.PrepareProjection(context.Background(), generation, coverage, chunks)
	if err != nil {
		t.Fatal(err)
	}

	ordered[0].Vector[0] = 0
	ordered[0].SourceIDs[0] = "changed"

	*ordered[0].ObservedAt = time.Time{}
	if chunks[0].Vector[0] != 1 || chunks[0].SourceIDs[0] != "z" || chunks[0].ObservedAt.IsZero() {
		t.Fatal("ordered generation leaked vector, membership, or timestamp ownership")
	}

	if err := semantic.CheckProjectionBounds(8192, 64<<20); err != nil {
		t.Fatal(err)
	}

	for _, size := range []struct {
		chunks int
		bytes  uint64
	}{{8193, 0}, {1, 64<<20 + 1}} {
		if err := semantic.CheckProjectionBounds(size.chunks, size.bytes); !errors.Is(err, semantic.ErrCapacity) {
			t.Fatalf("bounds(%d, %d) = %v", size.chunks, size.bytes, err)
		}
	}

	generation.Scope.Key = strings.Repeat("x", 4097)

	_, err = semantic.PreflightStage(context.Background(), generation, nil, nil)
	if !errors.Is(err, semantic.ErrInvalid) {
		t.Fatalf("oversized scope = %v", err)
	}
}
