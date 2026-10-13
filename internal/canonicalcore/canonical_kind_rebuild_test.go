package canonicalcore_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	index "github.com/baldaworks/boyan/index/bleve"
	core "github.com/baldaworks/boyan/internal/canonicalcore"
	runtime "github.com/baldaworks/boyan/internal/projection"
	syncer "github.com/baldaworks/boyan/internal/projectionsync"
	m "github.com/baldaworks/boyan/pkg/memory"
	engine "github.com/blevesearch/bleve/v2"
)

func TestKindFilterSurvivesOldSchemaRebuild(t *testing.T) {
	t.Parallel()
	reader, _, turn, _, store := nativeFlow(t)
	ingestKindFixture(t, store, turn)
	assertKindCanonicalPages(t, store, turn.Scope)
	root := t.TempDir()
	projection, err := index.Open(root)
	requireNoError(t, err, "projection: %v")
	applier, err := index.NewBleveCanonicalApplier(projection, reader)
	requireNoError(t, err, "applier: %v")
	coordinator, err := syncer.NewProjectionCoordinator(store, store, applier, 128)
	requireNoError(t, err, "coordinator: %v")
	manifest, err := coordinator.Sync(context.Background(), turn.Scope, "bleve", "kind-generation")
	requireNoError(t, err, "sync: %v")
	assertRebuiltKind(t, projection, reader, turn.Scope)
	requireNoError(t, projection.Close(), "close: %v")

	replaceWithOldMapping(t, root, manifest.GenerationID)
	reopened, err := index.Open(root)
	requireNoError(t, err, "reopen: %v")
	t.Cleanup(func() { requireNoError(t, reopened.Close(), "close: %v") })

	rebuilt, err := index.NewBleveCanonicalApplier(reopened, reader)
	requireNoError(t, err, "rebuild applier: %v")
	resumed, err := rebuilt.ResumeProjection(context.Background(), turn.Scope, manifest.GenerationID)
	requireNoError(t, err, "resume: %v")

	if resumed {
		t.Fatal("v1 generation accepted as compatible")
	}

	runner, err := runtime.NewProjectionRuntime(store, store, rebuilt, "bleve", 128)
	requireNoError(t, err, "runtime: %v")
	requireNoError(t, runner.Sync(context.Background(), turn.Scope), "rebuild without new changes: %v")
	final, found, err := store.LoadActiveProjectionManifest(context.Background(), turn.Scope, "bleve")
	requireNoError(t, err, "checkpoint: %v")

	if !found || final.GenerationID == manifest.GenerationID || final.Watermark != manifest.Watermark {
		t.Fatalf("invalid rebuilt manifest: %+v",
			final)
	}

	assertRebuiltKind(t, reopened, reader, turn.Scope)
	assertKindCanonicalPages(t, store, turn.Scope)
}

func assertRebuiltKind(t *testing.T,
	projection *index.BleveRecallProjection,
	reader m.RecallCanonicalReader,
	scope m.Scope) {
	t.Helper()

	for _, kind := range []m.MemoryKind{m.MemoryKindState, m.MemoryKindEvent} {
		hits,
			err := projection.SearchRecall(context.Background(),
			m.RecallRequest{Scope: scope,
				Query: "needle",
				Kind:  &kind,
				Limit: 10})
		requireNoError(t, err, "filtered search: %v")

		if len(hits) == 0 {
			t.Fatalf("kind %s lost after rebuild", kind)
		}

		ids := make([]string, 0, len(hits))
		for _, hit := range hits {
			ids = append(ids, hit.RevisionID)
		}

		records, err := reader.LoadRecallRecords(context.Background(), scope, ids)
		requireNoError(t, err, "hydrate kind: %v")

		if len(records) != len(hits) {
			t.Fatal("projection returned noncanonical hits")
		}

		for _, record := range records {
			if record.Kind != kind {
				t.Fatalf("filter %s included %s", kind, record.Kind)
			}
		}
	}
}

func ingestKindFixture(t *testing.T, store m.CanonicalStore, turn m.Turn) {
	t.Helper()

	extractor := canonicalExtractorFunc(func(ctx context.Context,
		request m.CanonicalExtractionRequest) ([]m.SemanticCandidate,
		error) {
		candidates, err := extractTestStateCandidate(ctx, request)
		if err != nil {
			return nil, err
		}

		candidates[0].Predicate = request.Turn.SourceTurnID
		if request.Turn.SourceTurnID == "event" {
			candidates[0].Kind = m.MemoryKindEvent
			candidates[0].Memory.Kind = m.MemoryKindEvent
		}

		return candidates, nil
	})
	processor, err := core.NewCanonicalTurnProcessor(store, extractor, m.PolicyRegistry{Version: "policy-v1"})
	requireNoError(t, err, "processor: %v")

	for i := range 257 {
		id := fmt.Sprintf("extra-%03d", i)
		if i == 256 {
			id = "event"
		}

		next, turnErr := m.NewTurn(turn.Scope, turn.Conversation, id, turn.CompletedAt, "needle memory", "OK")
		requireNoError(t, turnErr, "turn: %v")

		outcome, processErr := processor.ProcessTurn(context.Background(), next, testProcessorDerivation())
		turnErr = processErr

		if len(outcome.RevisionIDs) != 1 {
			t.Fatalf("ingest %s outcome: %+v", id, outcome)
		}

		requireNoError(t, turnErr, "process: %v")
	}
}

func replaceWithOldMapping(t *testing.T, root, generationID string) {
	t.Helper()

	oldPath := filepath.Join(root, "generations", generationID)
	requireNoError(t, os.RemoveAll(oldPath), "replace disposable fixture: %v")

	oldMapping := engine.NewIndexMapping()
	oldMapping.DefaultMapping = engine.NewDocumentStaticMapping()
	oldMapping.DefaultMapping.AddFieldMappingsAt("scope_key", engine.NewKeywordFieldMapping())
	oldIndex, err := engine.New(oldPath, oldMapping)
	requireNoError(t, err, "old physical mapping: %v")
	requireNoError(t, oldIndex.Close(), "old index close: %v")

	requireNoError(t,
		os.WriteFile(filepath.Join(root,
			"generations",
			generationID,
			".committed"),
			[]byte("boyan-bleve/v1\n"),
			0o600),
		"old marker: %v")
}

func assertKindCanonicalPages(t *testing.T, store m.CanonicalStore, scope m.Scope) {
	t.Helper()

	first, err := store.ScanActiveMemory(context.Background(), m.ActiveMemoryScanRequest{Scope: scope, Limit: 256})
	requireNoError(t, err, "first page: %v")

	if len(first) != 256 {
		t.Fatalf("first page: %d", len(first))
	}

	for _, active := range first {
		if active.Item.Kind == m.MemoryKindEvent {
			t.Fatal("event fixture is not beyond first fallback page")
		}
	}

	second,
		err := store.ScanActiveMemory(context.Background(),
		m.ActiveMemoryScanRequest{Scope: scope,
			AfterItemID: first[255].Item.ItemID,
			Limit:       256})
	requireNoError(t, err, "second page: %v")

	if len(second) != 2 || second[1].Item.Kind != m.MemoryKindEvent {
		t.Fatalf("canonical count or late event lost: %+v",
			second)
	}
}
