//nolint:testpackage // Reuse the canonical/manifest fixtures for real Bleve and vector-store integration.
package semanticprojection

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	bleveindex "github.com/baldaworks/boyan/index/bleve"
	"github.com/baldaworks/boyan/internal/embedding"
	"github.com/baldaworks/boyan/pkg/memory"
)

func TestHybridRetrievesSemanticOnlyAndOverlapBeforeCallerLimit(t *testing.T) {
	hybrid, _, _, request := hybridFixture(t)
	for range 3 {
		hits, err := hybrid.SearchRecall(context.Background(), request)
		if err != nil || len(hits) != 2 || hits[0].RevisionID != "revision-a" || hits[1].RevisionID != "revision-b" {
			t.Fatalf("hybrid candidates = %+v, %v", hits, err)
		}
	}

	request.MinScopeChangeSeq = 7
	if hits, err := hybrid.SearchRecallConsistent(context.Background(), request); err != nil || len(hits) != 2 {
		t.Fatalf("shared view = %+v, %v", hits, err)
	}
}

func TestHybridIncompatibleViewsDegradeOnlyOrdinaryRecall(t *testing.T) {
	cases := map[string]func(*LegacyHybrid, *fakeManifestSource, memory.RecallRequest){
		"cold": func(h *LegacyHybrid, _ *fakeManifestSource, _ memory.RecallRequest) { h.store = openSemanticStore(t) },
		"wrong space": func(h *LegacyHybrid, _ *fakeManifestSource, _ memory.RecallRequest) {
			h.spaceID = strings.Repeat("f", 64)
		},
		"wrong dimensions": func(h *LegacyHybrid, _ *fakeManifestSource, _ memory.RecallRequest) { h.space.Dimensions = 3 },
		"watermark mismatch": func(_ *LegacyHybrid, m *fakeManifestSource, r memory.RecallRequest) {
			m.set(activeManifest(r.Scope, "hybrid-generation", 8))
		},
		"rebuilding": func(_ *LegacyHybrid, m *fakeManifestSource, r memory.RecallRequest) {
			manifest := activeManifest(r.Scope, "hybrid-generation", 7)
			manifest.Status = memory.ProjectionGenerationBuilding
			m.set(manifest)
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			hybrid, _, manifests, request := hybridFixture(t)
			change(hybrid, manifests, request)

			hits, err := hybrid.SearchRecall(context.Background(), request)
			if err != nil || len(hits) != 1 || hits[0].RevisionID != "revision-a" {
				t.Fatalf("incompatible fallback: %+v, %v", hits, err)
			}

			request.MinScopeChangeSeq = 7
			_, err = hybrid.SearchRecallConsistent(context.Background(), request)

			code, _, ok := memory.ClassifyError(err)
			if !ok || code != memory.CodeConflict {
				t.Fatalf("incompatible minimum: %v", err)
			}
		})
	}
}

func TestHybridCertifiesEmptyAndPropagatesCanonicalFailures(t *testing.T) {
	hybrid, _, manifests, request := hybridFixture(t)
	request.SourceID = "absent-source"
	request.MinScopeChangeSeq = 7

	hits, err := hybrid.SearchRecallConsistent(context.Background(), request)
	if err != nil || len(hits) != 0 {
		t.Fatalf("certified empty: %+v, %v", hits, err)
	}

	request.MinScopeChangeSeq = 8
	_, err = hybrid.SearchRecallConsistent(context.Background(), request)

	code, _, ok := memory.ClassifyError(err)
	if !ok || code != memory.CodeConflict {
		t.Fatalf("behind minimum: %v", err)
	}

	failure := memory.RetryableError(memory.CodeStoreFailure, "checkpoint failure", nil)
	manifests.err = failure

	request.MinScopeChangeSeq = 0
	if _, err = hybrid.SearchRecall(context.Background(), request); !errors.Is(err, failure) {
		t.Fatalf("canonical failure hidden: %v", err)
	}
}

func TestHybridRejectsInvalidInputAndMalformedQueryVectors(t *testing.T) {
	hybrid, _, _, request := hybridFixture(t)
	for _, query := range []string{"", strings.Repeat("x", 4097), string([]byte{0xff})} {
		bad := request

		bad.Query = query
		if _, err := hybrid.SearchRecall(context.Background(), bad); err == nil {
			t.Fatal("invalid input became fallback")
		}
	}

	for _, vectors := range [][][]float32{nil, {{0, 0}}, {{1}}, {{1, 0}, {1, 0}}} {
		hybrid.provider = queryProviderFunc(func(context.Context, []string) ([][]float32, error) { return vectors, nil })

		hits, err := hybrid.SearchRecall(context.Background(), request)
		if err != nil || len(hits) != 1 {
			t.Fatalf("malformed query vector fallback: %+v, %v", hits, err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	hybrid.provider = queryProviderFunc(func(context.Context, []string) ([][]float32, error) {
		cancel()

		return [][]float32{{1, 0}}, nil
	})
	if _, err := hybrid.SearchRecall(ctx, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("provider cancellation hidden: %v", err)
	}
}

//nolint:cyclop // The table checks identical ordinary/minimum policies for each operational provider failure.
func TestHybridFailurePreservesLexicalAndMinimumConflicts(t *testing.T) {
	hybrid, provider, manifests, request := hybridFixture(t)
	for _, failure := range []error{
		&embedding.Error{Code: embedding.CodeUnavailable},
		&embedding.Error{Code: embedding.CodeDeadline},
		&embedding.Error{Code: embedding.CodeInvalidResponse},
	} {
		provider.err = failure

		hits, err := hybrid.SearchRecall(context.Background(), request)
		if err != nil || len(hits) != 1 || hits[0].RevisionID != "revision-a" {
			t.Fatalf("fallback = %+v, %v", hits, err)
		}

		request.MinScopeChangeSeq = 7
		_, err = hybrid.SearchRecallConsistent(context.Background(), request)

		code, class, ok := memory.ClassifyError(err)
		if !ok || code != memory.CodeConflict || class != memory.ErrorClassRetryable {
			t.Fatalf("minimum failure = %v", err)
		}

		request.MinScopeChangeSeq = 0
	}

	provider.err = nil

	manifests.set(activeManifest(request.Scope, "other", 8))

	hits, err := hybrid.SearchRecall(context.Background(), request)
	if err != nil || len(hits) != 1 {
		t.Fatalf("stale fallback = %+v, %v", hits, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := hybrid.SearchRecall(ctx, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
}

//nolint:funlen,cyclop // Resource creation and cleanup for real Bleve/store integration stay together.
func hybridFixture(t *testing.T, changes ...func([]memory.RecallRecord)) (
	*LegacyHybrid, *fakeProvider, *fakeManifestSource, memory.RecallRequest) {
	t.Helper()

	scope := testScope("hybrid")

	records := []memory.RecallRecord{
		testRecord(scope, "item-a", "revision-a", "memory", memory.SensitivityStandard, memory.RetentionClassStandard),
		testRecord(scope, "item-b", "revision-b", "journey", memory.SensitivityStandard, memory.RetentionClassStandard),
	}
	for _, change := range changes {
		change(records)
	}

	canonical := newFakeCanonical(scope, 7, records...)
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "hybrid-generation", 7)}

	p, err := bleveindex.Open(t.TempDir(), bleveindex.Config{Checkpoints: manifests, ProjectionID: "bleve"})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = p.Close() })

	generation, err := p.NewGeneration("hybrid-generation")
	if err != nil {
		t.Fatal(err)
	}

	for _, record := range records {
		document := memory.RecallProjectionDocument{Scope: scope, ItemID: record.ItemID,
			RevisionID: record.RevisionID, Revision: record.Revision, Kind: record.Kind, Text: record.Text,
			CreatedAt: record.CreatedAt, Temporal: record.Temporal, Sensitivity: record.Sensitivity,
			Retention: record.Retention, ScopeChangeSeq: 7}
		if err := generation.Index(context.Background(), document); err != nil {
			t.Fatal(err)
		}
	}

	if err := generation.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}

	_ = generation.Close()

	if err := p.ActivateGenerationForScope(context.Background(), scope, "hybrid-generation"); err != nil {
		t.Fatal(err)
	}

	provider := &fakeProvider{vector: []float32{1, 0}}
	store := openSemanticStore(t)

	manager, err := New(Dependencies{Canonical: canonical, Records: canonical, Manifests: manifests,
		Physical: p, Provider: provider, Store: store, ModelSpace: testModelSpace("model-a")})
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.Repair(context.Background(), scope); err != nil {
		t.Fatal(err)
	}

	hybrid, err := NewLegacyHybrid(p, store, provider, testModelSpace("model-a"))
	if err != nil {
		t.Fatal(err)
	}

	return hybrid, provider, manifests, memory.RecallRequest{Scope: scope, Query: "memory", Limit: 1}
}

func TestHybridHistoricalObservationSurvivesBuildAndStoreRoundTrip(t *testing.T) {
	hybrid, _, _, request := hybridFixture(t, func(records []memory.RecallRecord) {
		records[1].CreatedAt = time.Unix(2000, 0).UTC()
	})
	at := time.Unix(1000, 0).UTC()
	request.AsOf = &at
	request.MinScopeChangeSeq = 7

	hits, err := hybrid.SearchRecallConsistent(context.Background(), request)
	if err != nil || len(hits) != 2 || hits[1].RevisionID != "revision-b" {
		t.Fatalf("historical semantic candidate: %+v, %v", hits, err)
	}
}
