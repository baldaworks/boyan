//go:build integration

package standaloneruntime //nolint:testpackage // Proves the actual runtime composition binds real canonical authority.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/baldaworks/boyan/index/sqlite"
	"github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

func TestStandaloneCanonicalPrivacyBindingUsesRealAuthority(t *testing.T) {
	forEachSQLRuntimeConfig(t, func(t *testing.T, selection Config) {
		cfg := enabledLifecycleConfig(t, &lifecycleEmbeddingProvider{})
		cfg.Canonical = selection.Canonical
		runtime, err := New(cfg)
		requireNoError(t, err, "new runtime: %v")
		t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
		turn := seedSemanticTurn(t, runtime, "privacy-binding")
		startSQLComponents(t, runtime)
		waitSemanticState(t, runtime, turn.Scope, "hybrid-ready")
		// A second applier can share exactly the canonical instance already bound
		// by openStandaloneStorage, without changing its staging ownership.
		_, err = sqlite.NewSQLiteCanonicalApplier(runtime.service.index, runtime.service.reader, runtime.service.store)
		requireNoError(t, err, "same real authority rebind: %v")

		record := privacyRuntimeRecord(t, runtime, turn.Scope)
		id := record.RevisionID
		_, err = runtime.ForgetScope(t.Context(), memory.ForgetScopeCommand{
			SchemaVersion: memory.DerivedSchemaVersionV1, Scope: turn.Scope,
			RequestID: "privacy-binding-forget", ForgottenAt: time.Now().UTC(),
		})
		requireNoError(t, err, "forget canonical identity: %v")
		denied, err := runtime.service.store.IsRevisionDenied(t.Context(), turn.Scope, id)
		requireNoError(t, err, "real revision deny: %v")

		if !denied {
			t.Fatal("control did not establish real canonical denial")
		}

		manifest, found, err := runtime.service.store.LoadActiveProjectionManifest(t.Context(), turn.Scope, "sqlite")
		requireNoError(t, err, "current lexical source: %v")

		if !found {
			t.Fatal("missing current certified lexical source")
		}

		_, found, err = runtime.service.index.LoadActivePhysicalProjectionManifest(t.Context(), turn.Scope, "sqlite")
		requireNoError(t, err, "certify current physical source: %v")

		if !found {
			t.Fatal("late Stage fixture source is not physically certifiable")
		}

		g, coverage, chunks := lateDeniedPrivacyFixture(record, manifest)
		_, _, err = semantic.PrepareProjection(t.Context(), g, coverage, chunks)
		requireNoError(t, err, "late Stage portable admission control: %v")
		// The physical source is current, not the obsolete source before forget.
		// Only the real canonical identity authority can reject this valid late input.
		_, err = runtime.service.index.Stage(t.Context(), g, coverage, chunks)
		if !errors.Is(err, semantic.ErrConflict) {
			t.Fatalf("real denial was not invoked on runtime Store: %v", err)
		}

		db := openRuntimeSQL(t, runtime)
		defer func() { _ = db.Close() }()

		var count int

		err = db.QueryRowContext(t.Context(), "SELECT count(*) FROM semantic_build WHERE build_id=?", g.BuildID).Scan(&count)
		if err != nil || count != 0 {
			t.Fatalf("denied candidate durably claimed: %d %v", count, err)
		}
	})
}

func lateDeniedPrivacyFixture(record memory.RecallRecord, manifest memory.ProjectionManifest) (
	semantic.Generation, []semantic.CoverageRecord, []semantic.ProjectionChunk,
) {
	g := semantic.Generation{Scope: manifest.Scope, BuildID: "late-real-canonical-denial",
		LexicalGenerationID: manifest.GenerationID,
		LexicalWatermark:    manifest.Watermark, ModelSpace: strings.Repeat("a", 64),
		Dimensions: 2, ReadyAt: time.Now().UTC()}
	c := semantic.ProjectionChunk{Scope: g.Scope, BuildID: g.BuildID, LexicalGenerationID: g.LexicalGenerationID,
		LexicalWatermark: g.LexicalWatermark, ModelSpace: g.ModelSpace, Dimensions: 2, ItemID: record.ItemID,
		RevisionID: record.RevisionID, Revision: record.Revision,
		ContentHash: strings.Repeat("b", 64), InputHash: strings.Repeat("c", 64),
		ScopeChangeSeq: record.ScopeChangeSeq, Kind: record.Kind, CreatedAt: record.CreatedAt,
		ObservedAt: &record.Temporal.ObservedAt, SourceIDs: record.SourceIDs, Vector: []float32{1, 0}}
	coverage := []semantic.CoverageRecord{{ItemID: c.ItemID, RevisionID: record.RevisionID, Revision: c.Revision,
		ContentHash: c.ContentHash, ChunkCount: 1}}

	return g, coverage, []semantic.ProjectionChunk{c}
}

func privacyRuntimeRecord(t *testing.T, runtime *Runtime, scope memory.Scope) memory.RecallRecord {
	t.Helper()
	scan, err := runtime.service.store.ScanActiveMemory(t.Context(),
		memory.ActiveMemoryScanRequest{Scope: scope, Limit: 1})
	requireNoError(t, err, "scan canonical revision: %v")

	if len(scan) != 1 {
		t.Fatal("missing canonical control")
	}

	id := scan[0].RevisionID
	records, err := runtime.service.reader.LoadRecallRecords(t.Context(), scope, []string{id})
	requireNoError(t, err, "hydrate control: %v")

	if len(records) != 1 {
		t.Fatal("missing hydrated control")
	}

	return records[0]
}
