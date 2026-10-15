//nolint:testpackage // Exercise the bounded prefilter/ranker seam without inventing a mutable store.
package semanticprojection

import (
	"context"
	"fmt"
	"testing"
	"time"

	semanticstore "github.com/baldaworks/boyan/index/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

func TestHybridPrefiltersBeforeCosine(t *testing.T) {
	hybrid, _, _, request := hybridFixture(t)
	at := time.Date(2026, 10, 15, 1, 0, 0, 0, time.UTC)
	request.AsOf = &at
	kind := memory.MemoryKindState
	category := memory.AtomCategory("fact")
	request.Kind = &kind
	request.Category = &category
	request.SourceID = "source-a"
	request.ConversationID = "conversation-a"
	request.MemoryKey = "memory-a"
	base := semanticstore.Chunk{Scope: request.Scope, ItemID: "item", RevisionID: "revision", Revision: 1,
		Kind: kind, Category: category, SourceIDs: []string{"source-a"}, ConversationIDs: []string{"conversation-a"},
		MemoryKey: "memory-a", CreatedAt: at.Add(-time.Hour), Vector: []float32{1, 0}}
	observed := at.Add(-time.Hour)
	base.ObservedAt = &observed

	cases := map[string]func(*semanticstore.Chunk){
		"scope":              func(c *semanticstore.Chunk) { c.Scope.Key = "foreign" },
		"kind":               func(c *semanticstore.Chunk) { c.Kind = memory.MemoryKindEvent },
		"category":           func(c *semanticstore.Chunk) { c.Category = "preference" },
		"source":             func(c *semanticstore.Chunk) { c.SourceIDs = nil },
		"conversation":       func(c *semanticstore.Chunk) { c.ConversationIDs = nil },
		"memory key":         func(c *semanticstore.Chunk) { c.MemoryKey = "different" },
		"future observed":    func(c *semanticstore.Chunk) { v := at.Add(time.Second); c.ObservedAt = &v },
		"future valid":       func(c *semanticstore.Chunk) { v := at.Add(time.Second); c.ValidFrom = &v },
		"valid end equality": func(c *semanticstore.Chunk) { c.ValidUntil = &at },
		"expiry equality":    func(c *semanticstore.Chunk) { c.ExpiresAt = &at },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			excluded := base
			change(&excluded)
			excluded.Vector = nil

			hits, err := hybrid.fuse(context.Background(), request, nil,
				[][]float32{{1, 0}}, []semanticstore.Chunk{excluded}, 20)
			if err != nil || len(hits) != 0 {
				t.Fatalf("excluded metadata reached cosine: %+v, %v", hits, err)
			}
		})
	}

	hits, err := hybrid.fuse(context.Background(), request, nil, [][]float32{{1, 0}}, []semanticstore.Chunk{base}, 20)
	if err != nil || len(hits) != 1 {
		t.Fatalf("matching filters: %+v, %v", hits, err)
	}
}

func TestHybridAsOfUsesObservedTimeIndependentlyOfCreation(t *testing.T) {
	hybrid, _, _, request := hybridFixture(t)
	at := time.Date(2026, 10, 15, 1, 0, 0, 0, time.UTC)
	request.AsOf = &at
	observed := at.Add(-time.Hour)
	chunk := semanticstore.Chunk{Scope: request.Scope, ItemID: "item", RevisionID: "revision", Revision: 1,
		CreatedAt: at.Add(time.Hour), ObservedAt: &observed, Vector: []float32{1, 0}}

	hits, err := hybrid.fuse(context.Background(), request, nil, [][]float32{{1, 0}}, []semanticstore.Chunk{chunk}, 20)
	if err != nil || len(hits) != 1 {
		t.Fatalf("eligible observation omitted by creation time: %+v, %v", hits, err)
	}

	chunk.ObservedAt = nil

	_, err = hybrid.fuse(context.Background(), request, nil, [][]float32{{1, 0}}, []semanticstore.Chunk{chunk}, 20)
	if err == nil {
		t.Fatal("legacy metadata falsely certified temporal eligibility")
	}
}

func TestHybridExactScanAndHydrationCandidateBounds(t *testing.T) {
	hybrid, _, _, request := hybridFixture(t)
	request.Limit = 100

	chunks := make([]semanticstore.Chunk, 8192)

	observed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range chunks {
		chunks[i] = semanticstore.Chunk{Scope: request.Scope, ItemID: fmt.Sprintf("item-%05d", i),
			RevisionID: fmt.Sprintf("dense-%05d", i), Revision: 1,
			CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Vector: []float32{1, 0}}
		chunks[i].ObservedAt = &observed
	}

	lexical := make([]memory.RecallProjectionHit, 101)
	for i := range lexical {
		lexical[i] = memory.RecallProjectionHit{Scope: request.Scope, ItemID: fmt.Sprintf("lexical-item-%03d", i),
			RevisionID: fmt.Sprintf("lexical-%03d", i), Revision: 1}
	}

	hits, err := hybrid.fuse(context.Background(), request, lexical, [][]float32{{1, 0}}, chunks, 100)
	if err != nil || len(hits) != 200 {
		t.Fatalf("exact bound: %d candidates, %v", len(hits), err)
	}

	for _, hit := range hits {
		if hit.RevisionID == "lexical-100" || hit.RevisionID == "dense-00100" {
			t.Fatalf("channel overrun: %+v", hit)
		}
	}

	_, err = hybrid.fuse(context.Background(), request, lexical, [][]float32{{1, 0}}, append(chunks, chunks[0]), 100)
	if err == nil {
		t.Fatal("one-over scan accepted")
	}
}

type queryProviderFunc func(context.Context, []string) ([][]float32, error)

type fullLexical struct{ HybridLexical }

func (f fullLexical) SearchRecall(context.Context, memory.RecallRequest) ([]memory.RecallProjectionHit, error) {
	return make([]memory.RecallProjectionHit, 256), nil
}

func TestHybridFallbackBoundsHydrationEvenAtLegacyMaximum(t *testing.T) {
	hybrid, _, _, request := hybridFixture(t)
	hybrid.lexical = fullLexical{hybrid.lexical}

	hits, err := hybrid.fallback(context.Background(), request)
	if err != nil || len(hits) != 200 {
		t.Fatalf("fallback hydration bound: %d, %v", len(hits), err)
	}
}

func (f queryProviderFunc) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	return f(ctx, inputs)
}

func TestHybridManifestSwitchDuringProviderNeverReleasesDense(t *testing.T) {
	for _, minimum := range []uint64{0, 7} {
		t.Run(fmt.Sprint(minimum), func(t *testing.T) {
			hybrid, _, manifests, request := hybridFixture(t)
			request.MinScopeChangeSeq = minimum
			hybrid.provider = queryProviderFunc(func(context.Context, []string) ([][]float32, error) {
				manifests.set(activeManifest(request.Scope, "hybrid-generation", 8))

				return [][]float32{{1, 0}}, nil
			})

			hits, err := hybrid.SearchRecall(context.Background(), request)
			if minimum == 0 {
				if err != nil || len(hits) != 1 || hits[0].RevisionID != "revision-a" {
					t.Fatalf("switch leaked dense: %+v, %v", hits, err)
				}
			} else {
				code, _, ok := memory.ClassifyError(err)
				if !ok || code != memory.CodeConflict {
					t.Fatalf("switch minimum: %v", err)
				}
			}
		})
	}
}
