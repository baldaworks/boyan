//nolint:testpackage // Exercise real recall, hybrid, manager and vector-store temporal filtering together.
package semanticprojection

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/baldaworks/boyan/internal/memoryread"
	"github.com/baldaworks/boyan/pkg/memory"
)

type delayedRecallReader struct{ memory.RecallCanonicalReader }

func (reader delayedRecallReader) CurrentScopeChangeSeq(ctx context.Context, scope memory.Scope) (uint64, error) {
	time.Sleep(2 * time.Second)

	return reader.RecallCanonicalReader.CurrentScopeChangeSeq(ctx, scope)
}

// Bleve's global analysis workers cannot run inside a synctest bubble. This
// adapter certifies an empty lexical channel so only semantic recall can pass.
type emptyCertifiedLexical struct{ *fakeManifestSource }

func (emptyCertifiedLexical) SearchRecall(context.Context, memory.RecallRequest) ([]memory.RecallProjectionHit, error) {
	return nil, nil
}

func (lexical emptyCertifiedLexical) SearchRecallCertified(ctx context.Context,
	request memory.RecallRequest, _ int) (memory.CertifiedRecallResult, error) {
	manifest, certified, err := lexical.LoadActivePhysicalProjectionManifest(ctx, request.Scope, "bleve")

	return memory.CertifiedRecallResult{Manifest: manifest, Certified: certified}, err
}

func temporalHybridFixture(t *testing.T) (*LegacyHybrid, *fakeCanonical, memory.RecallRequest) {
	t.Helper()

	scope := testScope("temporal")
	record := testRecord(scope, "item", "revision", "journey", memory.SensitivityStandard, memory.RetentionClassStandard)
	expires := time.Now().Add(time.Second)
	record.Temporal.ExpiresAt = &expires
	record.SourceIDs = []string{"source"}

	evidence, err := memory.NewEvidenceRef("source", "message", memory.MessageRoleUser,
		"journey", 0, 7, memory.AssertionModeUser)
	if err != nil {
		t.Fatal(err)
	}

	record.Evidence = []memory.EvidenceRef{evidence}
	canonical := newFakeCanonical(scope, 7, record)
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "lexical", 7)}
	provider := &fakeProvider{vector: []float32{1, 0}}
	store := openSemanticStore(t)

	manager := newTestManager(t, canonical, manifests, provider, store, testModelSpace("model-a"))
	if err := manager.Repair(context.Background(), scope); err != nil {
		t.Fatal(err)
	}

	hybrid, err := NewLegacyHybrid(emptyCertifiedLexical{manifests}, store, provider, testModelSpace("model-a"))
	if err != nil {
		t.Fatal(err)
	}

	hybrid.provider = queryProviderFunc(func(context.Context, []string) ([][]float32, error) {
		time.Sleep(2 * time.Second)

		return [][]float32{{1, 0}}, nil
	})

	return hybrid, canonical, memory.RecallRequest{Scope: scope, Query: "memory", Limit: 10}
}

func TestHybridPinsEligibilityBeforeSlowDependencies(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hybrid, _, request := temporalHybridFixture(t)

		hits, err := hybrid.SearchRecall(context.Background(), request)
		if err != nil || len(hits) != 1 || hits[0].RevisionID != "revision" {
			t.Fatalf("candidate valid before inference lost during inference: %+v, %v", hits, err)
		}

		if request.AsOf != nil {
			t.Fatal("search mutated caller's eligibility time")
		}
	})
}

func TestRecallPinsHybridEligibilityBeforeSlowDependencies(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hybrid, canonical, request := temporalHybridFixture(t)

		service, err := memoryread.NewRecallService(delayedRecallReader{canonical}, hybrid)
		if err != nil {
			t.Fatal(err)
		}

		response, err := service.Search(context.Background(), request)
		if err != nil || len(response.Results) != 1 || response.Results[0].RevisionID != "revision" {
			t.Fatalf("candidate valid at recall start lost during dependency delay: %+v, %v", response, err)
		}

		if request.AsOf != nil {
			t.Fatal("search mutated caller's eligibility time")
		}
	})
}
