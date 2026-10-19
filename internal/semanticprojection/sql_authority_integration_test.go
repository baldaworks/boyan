//nolint:cyclop,gocognit,lll,testpackage // Finite real-owner integration cases keep privacy and physical-row assertions together.
package semanticprojection

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	index "github.com/baldaworks/boyan/index/sqlite"
	"github.com/baldaworks/boyan/internal/canonicalcore"
	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	"github.com/baldaworks/boyan/internal/forgetops"
	"github.com/baldaworks/boyan/internal/memoryops"
	"github.com/baldaworks/boyan/internal/memoryread"
	"github.com/baldaworks/boyan/internal/projection"
	"github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/postgres"
	"github.com/baldaworks/boyan/store/sqlite"
)

type searchSQLOwner interface {
	memory.CanonicalStore
	memory.ProjectionCheckpointStore
	forgetops.CanonicalForgetStore
	IsSourceDenied(ctx context.Context, scope memory.Scope, id string) (bool, error)
	IsRevisionDenied(ctx context.Context, scope memory.Scope, id string) (bool, error)
	DenySource(ctx context.Context, scope memory.Scope, id string, at time.Time) error
	DenyRevision(ctx context.Context, scope memory.Scope, id string, at time.Time) error
	PrepareSourcePrivacy(ctx context.Context) error
}

type searchSQLFixture struct {
	owner    searchSQLOwner
	reader   memory.RecallCanonicalReader
	root     string
	index    *index.Store
	runtime  *projection.ProjectionRuntime
	manager  *Manager
	db       *sql.DB
	turn     memory.Turn
	revision string
}

type searchSQLExtractor struct{}

func (searchSQLExtractor) ExtractCanonicalSemantics(_ context.Context, request memory.CanonicalExtractionRequest) ([]memory.SemanticCandidate, error) {
	message := request.Turn.Messages[0]

	evidence, err := memory.NewEvidenceRef(request.Turn.SourceID, message.MessageID, message.Role, message.Text, 0, uint32(len(message.Text)), memory.AssertionModeUser)
	if err != nil {
		return nil, err
	}

	return []memory.SemanticCandidate{{Kind: memory.MemoryKindState, Subject: "native subject", Predicate: "memory",
		Memory: memory.MemoryCandidate{Kind: memory.MemoryKindState, Statement: message.Text,
			Temporal: memory.Temporal{ObservedAt: request.Turn.CompletedAt}, Evidence: []memory.EvidenceRef{evidence},
			Sensitivity: memory.SensitivityStandard, Retention: memory.RetentionClassStandard}}}, nil
}

func forSearchSQL(t *testing.T, run func(*testing.T, *storefixture.Fixture)) {
	t.Helper()

	for _, backend := range []canonicalsqltest.Backend{canonicalsqltest.SQLite, canonicalsqltest.Postgres} {
		t.Run(string(backend), func(t *testing.T) { run(t, storefixture.New(t, backend)) })
	}
}

func newSearchSQLFixture(t *testing.T, fixture *storefixture.Fixture, embeddings bool) *searchSQLFixture {
	t.Helper()

	f := &searchSQLFixture{owner: fixture.Store.(searchSQLOwner), root: t.TempDir()}
	if fixture.Backend == canonicalsqltest.SQLite {
		f.root = filepath.Dir(filepath.Dir(fixture.Database.DSN))
	}

	if err := os.Chmod(f.root, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := f.owner.PrepareSourcePrivacy(t.Context()); err != nil {
		t.Fatal(err)
	}

	switch owner := fixture.Store.(type) {
	case *sqlite.Store:
		f.reader = sqlite.NewCanonicalReader(owner)
	case *postgres.Store:
		f.reader = postgres.NewCanonicalReader(owner)
	}

	processor, err := canonicalcore.NewCanonicalTurnProcessor(f.owner, searchSQLExtractor{}, memory.PolicyRegistry{Version: "policy-v1"})
	if err != nil {
		t.Fatal(err)
	}

	f.turn, err = memory.NewTurn(memory.Scope{Key: "test:native-search", Kind: memory.ScopeKindPersonal},
		memory.ConversationRef{ConversationID: "conversation", AgentConversationID: "agent"}, "turn", time.Now().UTC().Add(-time.Hour), "eligible native memory", "OK")
	if err != nil {
		t.Fatal(err)
	}

	outcome, err := processor.ProcessTurn(t.Context(), f.turn, memory.LegacyDerivationRef())
	if err != nil || len(outcome.RevisionIDs) != 1 {
		t.Fatalf("native ingest=%+v %v", outcome, err)
	}

	f.revision = outcome.RevisionIDs[0]
	f.open(t, embeddings)

	if err := f.runtime.Sync(t.Context(), f.turn.Scope); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { f.close(t) })

	return f
}

func (f *searchSQLFixture) open(t *testing.T, embeddings bool) {
	t.Helper()

	var err error

	f.index, err = index.Open(t.Context(), f.root, index.Options{EmbeddingsEnabled: embeddings})
	if err != nil {
		t.Fatal(err)
	}

	applier, err := index.NewSQLiteCanonicalApplier(f.index, f.reader, f.owner)
	if err != nil {
		t.Fatal(err)
	}

	f.runtime, err = projection.NewProjectionRuntime(f.owner, f.owner, applier, "sqlite", 128)
	if err != nil {
		t.Fatal(err)
	}

	f.db, err = sql.Open("sqlite", filepath.Join(f.root, "search", "search.sqlite"))
	if err != nil {
		t.Fatal(err)
	}

	if embeddings {
		f.manager, err = New(Dependencies{Canonical: f.owner, Records: f.reader, Manifests: f.owner,
			Physical: f.index, Provider: &fakeProvider{vector: []float32{1, 0}}, Store: f.index,
			ModelSpace: testModelSpace("native-model"), LexicalProjectionID: "sqlite", Lifecycle: f.index})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func (f *searchSQLFixture) close(t *testing.T) {
	t.Helper()

	if f.manager != nil {
		if err := f.manager.Close(context.Background()); err != nil {
			t.Error(err)
		}

		f.manager = nil
	}

	if f.runtime != nil {
		if err := f.runtime.Close(context.Background()); err != nil {
			t.Error(err)
		}

		f.runtime = nil
	}

	if f.db != nil {
		if err := f.db.Close(); err != nil {
			t.Error(err)
		}

		f.db = nil
	}

	if f.index != nil {
		if err := f.index.Close(); err != nil {
			t.Error(err)
		}

		f.index = nil
	}
}

func (f *searchSQLFixture) exec(t *testing.T, query string) {
	t.Helper()

	if _, err := f.db.ExecContext(t.Context(), query); err != nil {
		t.Fatal(err)
	}
}

func (f *searchSQLFixture) count(t *testing.T, table string, want int) {
	t.Helper()

	var got int
	if err := f.db.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&got); err != nil {
		t.Fatal(err)
	}

	if got != want {
		t.Fatalf("%s rows=%d want=%d", table, got, want)
	}
}

func TestSQLAuthorityHybridAndUnavailableFallback(t *testing.T) {
	forSearchSQL(t, func(t *testing.T, fixture *storefixture.Fixture) {
		f := newSearchSQLFixture(t, fixture, true)
		if err := f.manager.Repair(t.Context(), f.turn.Scope); err != nil {
			t.Fatal(err)
		}

		hybrid, err := NewHybrid(HybridDependencies{Canonical: f.owner, Manifests: f.owner, Reader: f.index,
			Lexical: f.index, Provider: &fakeProvider{vector: []float32{1, 0}}, ModelSpace: testModelSpace("native-model")})
		if err != nil {
			t.Fatal(err)
		}

		service, err := memoryread.NewRecallService(f.reader, hybrid)
		if err != nil {
			t.Fatal(err)
		}

		request := memory.RecallRequest{Scope: f.turn.Scope, Query: "eligible", Limit: 10}

		result, err := service.Search(t.Context(), request)
		if err != nil || len(result.Results) != 1 || result.Results[0].Text != "eligible native memory" || result.Retrieval == nil || result.Retrieval.Effective != "hybrid" {
			t.Fatalf("native hybrid=%+v %v", result, err)
		}

		f.count(t, "semantic_chunk", 1)
		f.count(t, "semantic_chunk WHERE length(embedding)=8", 1)

		hybrid.provider = &fakeProvider{err: errors.New("controlled embedding unavailable")}

		result, err = service.Search(t.Context(), request)
		if err != nil || len(result.Results) != 1 || result.Results[0].RevisionID != f.revision || result.Retrieval == nil || result.Retrieval.Effective != "degraded" {
			t.Fatalf("native fallback=%+v %v", result, err)
		}

		if err := f.owner.DenySource(t.Context(), f.turn.Scope, f.turn.SourceID, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}

		result, err = service.Search(t.Context(), request)
		if len(result.Results) != 0 {
			t.Fatalf("stale native hit disclosed=%+v %v", result, err)
		}
	})
}

type searchSQLSync func(context.Context, memory.Scope) error

func (sync searchSQLSync) Sync(ctx context.Context, scope memory.Scope) error {
	return sync(ctx, scope)
}

func TestSQLAuthorityForgetRetryJoinsPhysicalRetirement(t *testing.T) {
	forSearchSQL(t, func(t *testing.T, fixture *storefixture.Fixture) {
		for _, kind := range []memory.ForgetKind{memory.ForgetKindSource, memory.ForgetKindScope} {
			t.Run(string(kind), func(t *testing.T) {
				// Each erase owns an independent native store, not a labelled synthetic authority.
				f := newSearchSQLFixture(t, storefixture.New(t, fixture.Backend), true)
				if err := f.manager.Repair(t.Context(), f.turn.Scope); err != nil {
					t.Fatal(err)
				}

				adapter, err := forgetops.NewCanonicalForgetAdapter(f.owner)
				if err != nil {
					t.Fatal(err)
				}

				service, err := memoryops.NewForgetServiceWithProjection(adapter, searchSQLSync(func(ctx context.Context, scope memory.Scope) error {
					if err := f.index.ReconcileCanonicalDenials(ctx, scope); err != nil {
						return err
					}

					return f.runtime.Sync(ctx, scope)
				}))
				if err != nil {
					t.Fatal(err)
				}

				at := time.Now().UTC()
				source := memory.SourceRef{Scope: f.turn.Scope, ExportID: f.turn.ExportID, ConversationID: f.turn.Conversation.ConversationID, SourceTurnID: f.turn.SourceTurnID}
				forget := func() (memory.ForgetOutcome, error) {
					if kind == memory.ForgetKindSource {
						return service.ForgetSource(t.Context(), memory.ForgetSourceCommand{SchemaVersion: memory.DerivedSchemaVersionV1, Source: source, ForgottenAt: at})
					}

					return service.ForgetScope(t.Context(), memory.ForgetScopeCommand{SchemaVersion: memory.DerivedSchemaVersionV1, Scope: f.turn.Scope, RequestID: "retry", ForgottenAt: at})
				}

				f.exec(t, "CREATE TRIGGER fail_retirement BEFORE DELETE ON semantic_chunk BEGIN SELECT RAISE(ABORT,'injected search retirement failure'); END")

				outcome, err := forget()
				if err == nil || outcome.RevisionCount != 1 {
					t.Fatalf("canonical success hid physical failure=%+v %v", outcome, err)
				}

				journal, found, err := f.owner.LoadScopeErase(t.Context(), f.turn.Scope, outcome.OperationID)
				if err != nil || !found || journal.Stage != memory.ScopeEraseComplete {
					t.Fatalf("canonical completion=%+v %v %v", journal, found, err)
				}

				f.count(t, "semantic_chunk", 1)
				f.exec(t, "DROP TRIGGER fail_retirement")

				replayed, err := forget()
				if err != nil || !reflect.DeepEqual(replayed, outcome) {
					t.Fatalf("retry=%+v original=%+v %v", replayed, outcome, err)
				}

				for _, table := range []string{"lexical_record", "lexical_source", "lexical_conversation", "lexical_fts", "semantic_chunk", "semantic_source", "semantic_conversation", "semantic_coverage"} {
					f.count(t, table, 0)
				}
			})
		}
	})
}

func TestSQLAuthoritySearchRebuildPreservesCanonicalHistory(t *testing.T) {
	forSearchSQL(t, func(t *testing.T, fixture *storefixture.Fixture) {
		f := newSearchSQLFixture(t, fixture, false)

		state, err := f.owner.LoadScopeState(t.Context(), f.turn.Scope)
		if err != nil {
			t.Fatal(err)
		}

		changes, err := f.owner.ScanScopeChanges(t.Context(), f.turn.Scope, 0, 128)
		if err != nil {
			t.Fatal(err)
		}

		f.close(t)

		if err := os.RemoveAll(filepath.Join(f.root, "search")); err != nil {
			t.Fatal(err)
		}

		if fixture.Backend == canonicalsqltest.SQLite {
			if _, err := os.Stat(filepath.Join(f.root, "canonical", "canonical.sqlite")); err != nil {
				t.Fatal(err)
			}
		}

		f.open(t, false)

		if err := f.runtime.Start(t.Context()); err != nil {
			t.Fatal(err)
		}

		service, err := memoryread.NewRecallService(f.reader, f.index)
		if err != nil {
			t.Fatal(err)
		}

		result, err := service.Search(t.Context(), memory.RecallRequest{Scope: f.turn.Scope, Query: "eligible", Limit: 10})
		if err != nil || len(result.Results) != 1 || result.Results[0].RevisionID != f.revision {
			t.Fatalf("rebuilt lexical=%+v %v", result, err)
		}

		after, err := f.owner.LoadScopeState(t.Context(), f.turn.Scope)
		if err != nil || after != state {
			t.Fatalf("rebuild mutated canonical state=%+v before=%+v %v", after, state, err)
		}

		afterChanges, err := f.owner.ScanScopeChanges(t.Context(), f.turn.Scope, 0, 128)
		if err != nil || !reflect.DeepEqual(afterChanges, changes) {
			t.Fatalf("rebuild mutated canonical changes=%+v %v", afterChanges, err)
		}
	})
}

type searchSQLHydration struct {
	memory.RecallCanonicalReader

	after func()
}

func (reader searchSQLHydration) LoadRecallRecords(ctx context.Context, scope memory.Scope, ids []string) ([]memory.RecallRecord, error) {
	records, err := reader.RecallCanonicalReader.LoadRecallRecords(ctx, scope, ids)
	if err == nil && len(records) > 0 {
		reader.after()
	}

	return records, err
}

//nolint:funlen // Each real hydration, durable denial and cancellation result must be checked in the same leaf.
func TestSQLAuthorityDenialAfterHydrationBeforePopulation(t *testing.T) {
	forSearchSQL(t, func(t *testing.T, fixture *storefixture.Fixture) {
		for _, mode := range []string{"source", "revision", "cancel"} {
			t.Run(mode, func(t *testing.T) {
				native := fixture
				if mode != "source" {
					native = storefixture.New(t, fixture.Backend)
				}

				f := newSearchSQLFixture(t, native, false)

				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()

				calls := 0
				reader := searchSQLHydration{RecallCanonicalReader: f.reader, after: func() {
					calls++

					if mode == "revision" {
						if err := f.owner.DenyRevision(t.Context(), f.turn.Scope, f.revision, time.Now().UTC()); err != nil {
							t.Fatal(err)
						}
					} else {
						if err := f.owner.DenySource(t.Context(), f.turn.Scope, f.turn.SourceID, time.Now().UTC()); err != nil {
							t.Fatal(err)
						}
					}

					if mode == "cancel" {
						cancel()
					}
				}}

				applier, err := index.NewSQLiteCanonicalApplier(f.index, reader, f.owner)
				if err != nil {
					t.Fatal(err)
				}

				if err := applier.BeginProjectionRebuild(t.Context(), f.turn.Scope, "denied-after-hydration"); err != nil {
					t.Fatal(err)
				}

				err = applier.Apply(ctx, f.turn.Scope, "denied-after-hydration", nil, []memory.MemoryRevision{{RevisionID: f.revision}})
				if mode == "cancel" {
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("cancellation precedence lost: %v", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}

				if calls != 1 {
					t.Fatalf("actual canonical hydration callbacks=%d", calls)
				}

				var count int
				if err := f.db.QueryRowContext(t.Context(), "SELECT count(*) FROM lexical_record r JOIN lexical_build b USING(physical_build_id) WHERE b.generation_id='denied-after-hydration'").Scan(&count); err != nil || count != 0 {
					t.Fatalf("late denial populated rows=%d %v", count, err)
				}

				if err := applier.AbortProjectionSync(t.Context(), "denied-after-hydration"); err != nil {
					t.Fatal(err)
				}
			})
		}
	})
}

type noncomparableSQLAuthority struct {
	searchSQLOwner

	values []string
}

func TestSQLAuthorityRejectsChangedPrivacyIdentity(t *testing.T) {
	forSearchSQL(t, func(t *testing.T, fixture *storefixture.Fixture) {
		f := newSearchSQLFixture(t, fixture, false)
		if _, err := index.NewSQLiteCanonicalApplier(f.index, f.reader, f.owner); err != nil {
			t.Fatal(err)
		}

		if _, err := index.NewSQLiteCanonicalApplier(f.index, f.reader, (*sqlite.Store)(nil)); err == nil {
			t.Fatal("typed nil SQLite authority accepted")
		}

		if _, err := index.NewSQLiteCanonicalApplier(f.index, f.reader, (*postgres.Store)(nil)); err == nil {
			t.Fatal("typed nil PostgreSQL authority accepted")
		}

		if _, err := index.NewSQLiteCanonicalApplier(f.index, f.reader, noncomparableSQLAuthority{searchSQLOwner: f.owner}); err == nil {
			t.Fatal("noncomparable authority accepted")
		}

		changed := storefixture.New(t, fixture.Backend).Store.(searchSQLOwner)
		if _, err := index.NewSQLiteCanonicalApplier(f.index, f.reader, changed); err == nil {
			t.Fatal("changed comparable Store pointer accepted")
		}

		hits, err := f.index.SearchRecall(t.Context(), memory.RecallRequest{Scope: f.turn.Scope, Query: "eligible", Limit: 10})
		if err != nil || len(hits) != 1 || hits[0].RevisionID != f.revision {
			t.Fatalf("rejected binding damaged original authority: %+v %v", hits, err)
		}
	})
}
