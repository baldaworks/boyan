package bleve_test

import (
	"context"
	"fmt"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"testing"
	"time"
)

func TestCertifiedSearchReturnsExactPhysicalManifest(t *testing.T) {
	p, checkpoints, manifest, request := consistentProjectionFixture(t)

	result, err := p.SearchRecallCertified(context.Background(), request, 1)
	if err != nil || !result.Certified || result.Manifest != manifest || len(result.Hits) != 1 {
		t.Fatalf("certified search = %+v, %v", result, err)
	}

	checkpoints.manifest.GenerationID = "not-open"
	request.MinScopeChangeSeq = 0

	result, err = p.SearchRecallCertified(context.Background(), request, 1)
	if err != nil || result.Certified || len(result.Hits) != 1 {
		t.Fatalf("ordinary uncertified = %+v, %v", result, err)
	}

	request.MinScopeChangeSeq = 1
	if _, err = p.SearchRecallCertified(context.Background(), request, 1); err == nil {
		t.Fatal("uncertified minimum accepted")
	}
}

func TestCertifiedSearchDetectsManifestChangeWithinSearch(t *testing.T) {
	for _, minimum := range []uint64{0, 2} {
		t.Run(fmt.Sprint(minimum), func(t *testing.T) {
			p, checkpoints, manifest, request := consistentProjectionFixture(t)
			changed := manifest
			changed.UpdatedAt = manifest.UpdatedAt.Add(time.Nanosecond)
			checkpoints.second = &changed
			request.MinScopeChangeSeq = minimum

			result, err := p.SearchRecallCertified(context.Background(), request, 100)
			if minimum == 0 {
				if err != nil || result.Certified || len(result.Hits) != 1 {
					t.Fatalf("changed certificate: %+v, %v", result, err)
				}
			} else {
				code, _, ok := memory.ClassifyError(err)
				if !ok || code != memory.CodeConflict {
					t.Fatalf("changed minimum: %v", err)
				}
			}
		})
	}
}

func TestCertifiedSearchChannelDepthBoundsAndStableOrder(t *testing.T) {
	p, checkpoints, manifest, request := consistentProjectionFixture(t)
	manifest.GenerationID = "bounded"
	checkpoints.manifest = manifest
	generation, err := p.NewGeneration("bounded")
	requireNoError(t, err, "generation: %v")

	for i := range 101 {
		id := fmt.Sprintf("revision-%03d", i)
		requireNoError(t, generation.Index(context.Background(), recallProjectionDocument(request.Scope,
			id, id, "memory", manifest.UpdatedAt, 1)), "index: %v")
	}

	requireNoError(t, generation.Commit(context.Background()), "commit: %v")
	requireNoError(t, generation.Close(), "close: %v")
	requireNoError(t, p.ActivateGenerationForScope(context.Background(), request.Scope, "bounded"), "activate: %v")

	for range 2 {
		result, err := p.SearchRecallCertified(context.Background(), request, 100)
		if err != nil || !result.Certified || len(result.Hits) != 100 {
			t.Fatalf("bounded search: %d, %v", len(result.Hits), err)
		}

		if result.Hits[0].RevisionID != "revision-000" || result.Hits[99].RevisionID != "revision-099" {
			t.Fatalf("unstable ties: %+v", result.Hits)
		}
	}

	for _, depth := range []int{0, 101} {
		if _, err := p.SearchRecallCertified(context.Background(), request, depth); err == nil {
			t.Fatalf("invalid depth %d accepted", depth)
		}
	}
}
