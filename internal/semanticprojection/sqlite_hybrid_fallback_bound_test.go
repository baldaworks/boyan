package semanticprojection //nolint:testpackage // Preserve the independent fallback hydration cap against actual SQL.

import (
	"fmt"
	"testing"

	sqlitestore "github.com/baldaworks/boyan/index/sqlite"
	"github.com/baldaworks/boyan/pkg/memory"
)

//nolint:cyclop,funlen // Actual publication, lexical control and fallback share one bounded fixture.
func TestSQLiteHybridFallbackBoundsHydrationAtLexicalMaximum(t *testing.T) {
	f := newSQLiteManagerFixture(t)
	records := make([]memory.RecallRecord, 300)
	revisions := make([]memory.MemoryRevision, 300)

	for i := range records {
		id := fmt.Sprintf("fallback-%03d", i)
		records[i] = testRecord(f.scope, id, id, "eligible text",
			memory.SensitivityStandard, memory.RetentionClassStandard)
		revisions[i].RevisionID = id
	}

	f.canonical.replace(7, records...)
	manifest := f.checkpoints.manifest
	manifest.GenerationID = "sqlite-fallback-bound"
	f.checkpoints.set(manifest)

	applier, err := sqlitestore.NewSQLiteCanonicalApplier(f.store.Store, f.canonical, f.privacy)
	if err != nil {
		t.Fatal(err)
	}

	applier.ConfigureProjectionRetention(f.checkpoints, "sqlite")

	if err := applier.BeginProjectionRebuild(t.Context(), f.scope, manifest.GenerationID); err != nil {
		t.Fatal(err)
	}

	if err := applier.Apply(t.Context(), f.scope, manifest.GenerationID, nil, revisions); err != nil {
		t.Fatal(err)
	}

	if err := applier.CommitProjectionManifest(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}

	if err := applier.Activate(t.Context(), f.scope, manifest.GenerationID); err != nil {
		t.Fatal(err)
	}

	request := memory.RecallRequest{Scope: f.scope, Query: "eligible", Limit: 100}

	lexical, err := f.store.SearchRecall(t.Context(), request)
	if err != nil || len(lexical) != 256 {
		t.Fatalf("actual lexical maximum: hits=%d err=%v", len(lexical), err)
	}

	provider := &fakeProvider{vector: []float32{1, 0}}

	hybrid, err := NewHybrid(HybridDependencies{Canonical: f.canonical, Manifests: f.checkpoints,
		Reader: f.store, Lexical: f.store, Provider: provider, ModelSpace: testModelSpace("model-a")})
	if err != nil {
		t.Fatal(err)
	}

	hits, report, err := hybrid.SearchRecallWithReport(t.Context(), request)
	if err != nil || len(hits) != 200 {
		t.Fatalf("fallback hydration bound: hits=%d report=%#v err=%v", len(hits), report, err)
	}

	if hits[0].RevisionID != "fallback-000" || hits[199].RevisionID != "fallback-199" ||
		report.Effective != "degraded" || report.LexicalCandidates != 100 ||
		report.SemanticCandidates != 0 || report.ScannedChunks != 0 || len(provider.Inputs()) != 0 {
		t.Fatalf("fallback order/report/provider: hits=%#v report=%#v calls=%d", hits, report, len(provider.Inputs()))
	}
}
