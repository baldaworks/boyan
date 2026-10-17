package boyan_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	core "github.com/baldaworks/boyan/internal/canonicalcore"
	hostapi "github.com/baldaworks/boyan/pkg/boyan"
	m "github.com/baldaworks/boyan/pkg/memory"
	b "github.com/baldaworks/boyan/store/badger"
	engine "github.com/blevesearch/bleve/v2"
)

func TestPublicHostRecoversProjectionBeforeReady(t *testing.T) {
	for _, mode := range []string{"missing", "v1", "dirty", "never-created"} {
		t.Run(mode, func(t *testing.T) {
			config := testHostConfig(t)
			turn, err := m.NewTurn(m.Scope{Key: "startup:recovery", Kind: m.ScopeKindPersonal},
				m.ConversationRef{ConversationID: "conversation", AgentConversationID: "agent"},
				"turn", time.Now().UTC().Add(-time.Hour), "Я живу в Бишкеке", "OK")
			requireNoError(t, err, "turn: %v")
			initial := prepareStartupProjection(t, config, turn, mode)
			host, err := hostapi.New(context.Background(), config)
			requireNoError(t, err, "host: %v")
			t.Cleanup(func() { _ = host.Close() })
			requireNoError(t, host.Start(context.Background()), "start: %v")

			if !host.Ready() {
				t.Fatal("successful startup is not ready")
			}

			response, err := host.Search(context.Background(), m.RecallRequest{
				Scope: turn.Scope, Query: "живу", Limit: 10, MinScopeChangeSeq: initial.ChangeSeq,
			})
			requireNoError(t, err, "immediate fresh recall without ingest or manual sync: %v")

			if len(response.Results) != 1 || response.Results[0].Text != turn.Messages[0].Text {
				t.Fatalf("recovered recall: %+v", response)
			}

			requireNoError(t, host.Close(), "close: %v")
			assertStartupCanonicalUnchanged(t, config, turn.Scope, initial)
		})
	}
}

func prepareStartupProjection(
	t *testing.T, config hostapi.Config, turn m.Turn, mode string,
) m.CanonicalMutationOutcome {
	t.Helper()

	if mode == "never-created" {
		store, err := b.Open(filepath.Join(config.StateDir, "badger"))

		requireNoError(t, err, "store: %v")
		defer func() { requireNoError(t, store.Close(), "store close: %v") }()

		processor, err := core.NewCanonicalTurnProcessor(
			store, integrationExtractor{}, m.PolicyRegistry{Version: "policy-v1"},
		)
		requireNoError(t, err, "processor: %v")
		outcome, err := processor.ProcessTurn(context.Background(), turn, m.LegacyDerivationRef())
		requireNoError(t, err, "unprojected native turn: %v")

		return outcome
	}

	outcome := persistIntegrationTurn(t, config, turn)
	if mode == "missing" {
		requireNoError(t, os.Remove(filepath.Join(config.StateDir, "search", "search.sqlite")), "remove disposable index: %v")

		return outcome
	}

	store, err := b.Open(filepath.Join(config.StateDir, "badger"))
	requireNoError(t, err, "store: %v")
	manifest, found, err := store.LoadActiveProjectionManifest(context.Background(), turn.Scope, "sqlite")
	requireNoError(t, err, "manifest: %v")

	if !found {
		t.Fatal("native checkpoint missing")
	}

	if mode == "dirty" {
		requireNoError(t, store.MarkProjectionDirty(context.Background(), manifest), "mark dirty: %v")
	} else {
		// Old mapping bytes are an ignored legacy sibling after the cutover.
		// TASK016 may retire this engine-specific fixture after inventory review.
		replaceStartupV1Index(t, config, manifest.GenerationID)
	}

	requireNoError(t, store.Close(), "store close: %v")

	return outcome
}

func replaceStartupV1Index(t *testing.T, config hostapi.Config, generation string) {
	t.Helper()

	path := filepath.Join(config.StateDir, "bleve", "generations", generation)
	requireNoError(t, os.MkdirAll(filepath.Dir(path), 0o700), "legacy parent: %v")

	mapping := engine.NewIndexMapping()
	mapping.DefaultMapping = engine.NewDocumentStaticMapping()
	mapping.DefaultMapping.AddFieldMappingsAt("scope_key", engine.NewKeywordFieldMapping())
	old, err := engine.New(path, mapping)
	requireNoError(t, err, "old mapping: %v")
	requireNoError(t, old.Close(), "old index close: %v")
	requireNoError(t, os.WriteFile(filepath.Join(path, ".committed"), []byte("boyan-bleve/v1\n"), 0o600), "old marker: %v")
}

func assertStartupCanonicalUnchanged(
	t *testing.T, config hostapi.Config, scope m.Scope, initial m.CanonicalMutationOutcome,
) {
	t.Helper()

	store, err := b.Open(filepath.Join(config.StateDir, "badger"))

	requireNoError(t, err, "reopen: %v")
	defer func() { requireNoError(t, store.Close(), "store close: %v") }()

	state, err := store.LoadScopeState(context.Background(), scope)
	requireNoError(t, err, "scope: %v")

	if state.Version != initial.ScopeVersion || state.ChangeSeq != initial.ChangeSeq {
		t.Fatalf("startup changed canonical history: %+v; initial %+v", state, initial)
	}
}
