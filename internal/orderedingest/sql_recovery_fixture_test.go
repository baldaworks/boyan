package orderedingest_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/boundarycore"
	"github.com/baldaworks/boyan/internal/canonicalcore"
	"github.com/baldaworks/boyan/internal/canonicalops"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	"github.com/baldaworks/boyan/internal/orderedingest"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/postgres"
	"github.com/baldaworks/boyan/store/sqlite"
)

type sqlIngestStore interface {
	storefixture.Store
	memory.IngestSequenceStore
	canonicalcore.TurnStore
	canonicalops.BoundaryCanonicalStore
	PrepareSourcePrivacy(ctx context.Context) error
	ScanScopeChanges(ctx context.Context, scope memory.Scope, after uint64, limit uint32) ([]memory.ScopeChange, error)
	memory.CanonicalTurnIdentityStore
}

type sqlProcessingCounts struct {
	turnAttempts, extraction, scenario, profile int
	allowMultiple                               bool
	firstStarted, releaseFirst                  chan struct{}
}

type sqlCountingTurn struct {
	*canonicalcore.CanonicalTurnProcessor

	counts *sqlProcessingCounts
}

func (p *sqlCountingTurn) ProcessTurn(ctx context.Context, turn memory.Turn,
	derivation memory.DerivationRef) (memory.CanonicalMutationOutcome, error) {
	p.counts.turnAttempts++
	if p.counts.turnAttempts == 1 && p.counts.firstStarted != nil {
		close(p.counts.firstStarted)

		select {
		case <-p.counts.releaseFirst:
		case <-ctx.Done():
			return memory.CanonicalMutationOutcome{}, ctx.Err()
		}
	}

	return p.CanonicalTurnProcessor.ProcessTurn(ctx, turn, derivation)
}

type sqlCountingExtraction struct{ counts *sqlProcessingCounts }

func (e sqlCountingExtraction) ExtractCanonicalSemantics(_ context.Context,
	request memory.CanonicalExtractionRequest) ([]memory.SemanticCandidate, error) {
	e.counts.extraction++
	if e.counts.extraction > 1 && !e.counts.allowMultiple {
		return nil, memory.RetryableError(memory.CodeModelFailure, "fixture provider unavailable after commit", nil)
	}

	message := request.Turn.Messages[0]

	evidence, err := memory.NewEvidenceRef(request.Turn.SourceID, message.MessageID, message.Role,
		message.Text, 0, uint32(len(message.Text)), memory.AssertionModeUser)
	if err != nil {
		return nil, err
	}

	return []memory.SemanticCandidate{{Kind: memory.MemoryKindState, Subject: "owner", Predicate: "lives in",
		Memory: memory.MemoryCandidate{Kind: memory.MemoryKindState, Statement: message.Text,
			Temporal: memory.Temporal{ObservedAt: request.Turn.CompletedAt}, Evidence: []memory.EvidenceRef{evidence},
			Sensitivity: memory.SensitivityStandard, Retention: memory.RetentionClassStandard}}}, nil
}

type sqlCountingSynthesis struct{ counts *sqlProcessingCounts }

func (s sqlCountingSynthesis) SynthesizeScenarios(context.Context,
	memory.ScenarioSynthesisRequest) ([]memory.ScenarioCandidate, error) {
	s.counts.scenario++
	if s.counts.scenario > 1 {
		return nil, memory.RetryableError(memory.CodeModelFailure, "fixture scenarios unavailable after commit", nil)
	}

	return nil, nil
}

func (s sqlCountingSynthesis) SynthesizeProfile(context.Context,
	memory.ProfileSynthesisRequest) (*memory.ProfileCandidate, error) {
	s.counts.profile++
	if s.counts.profile > 1 {
		return nil, memory.RetryableError(memory.CodeModelFailure, "fixture profile unavailable after commit", nil)
	}

	return nil, nil
}

func newSQLIngestRuntime(t *testing.T, store sqlIngestStore, counts *sqlProcessingCounts,
	boundary bool, projections ...orderedingest.ProjectionSyncer) *orderedingest.SequencedIngestRuntime {
	t.Helper()

	if err := store.PrepareSourcePrivacy(t.Context()); err != nil {
		t.Fatal(err)
	}

	var (
		turn      orderedingest.TurnProcessor
		processor orderedingest.BoundaryProcessor
	)

	if boundary {
		processor = newSQLBoundaryProcessor(t, store, counts)
	} else {
		core, err := canonicalcore.NewCanonicalTurnProcessor(store, sqlCountingExtraction{counts: counts},
			memory.PolicyRegistry{Version: "policy-v1"})
		if err != nil {
			t.Fatal(err)
		}

		turn = &sqlCountingTurn{CanonicalTurnProcessor: core, counts: counts}
	}

	var projection orderedingest.ProjectionSyncer
	if len(projections) > 0 {
		projection = projections[0]
	}

	runtime, err := orderedingest.NewSequencedIngestRuntime(store, store, turn, memory.LegacyDerivationRef(),
		projection, processor, nil, orderedingest.SequencedIngestConfig{ProjectionRetryDelay: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}

	if err := runtime.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	return runtime
}

func newSQLBoundaryProcessor(t *testing.T, store sqlIngestStore,
	counts *sqlProcessingCounts) *boundarycore.BoundaryProcessor {
	t.Helper()

	var reader canonicalops.BoundaryCanonicalReader

	switch owner := store.(type) {
	case *sqlite.Store:
		reader = sqlite.NewCanonicalReader(owner)
	case *postgres.Store:
		reader = postgres.NewCanonicalReader(owner)
	default:
		t.Fatal("unexpected SQL owner")
	}

	adapter, err := canonicalops.NewCanonicalBoundaryStore(store, reader)
	requireSQLSuccess(t, err)

	synthesizer := sqlCountingSynthesis{counts: counts}
	processor, err := boundarycore.NewBoundaryProcessorWithClock(adapter, synthesizer, synthesizer,
		memory.LegacyDerivationRef(), func() time.Time { return sqlIngestTime().Add(time.Hour) })
	requireSQLSuccess(t, err)

	return processor
}

func requireSQLSuccess(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}
}

func reopenSQLIngestStore(t *testing.T, fixture *storefixture.Fixture,
	runtime *orderedingest.SequencedIngestRuntime, store sqlIngestStore) sqlIngestStore {
	t.Helper()
	requireSQLSuccess(t, runtime.Close(t.Context()))
	requireSQLSuccess(t, store.Close())
	opened, err := fixture.Open(t.Context())
	requireSQLSuccess(t, err)

	return opened.(sqlIngestStore)
}

func sqlIngestTime() time.Time { return time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC) }

func sqlIngestTurn(t *testing.T) memory.SequencedTurn {
	t.Helper()

	turn, err := memory.NewTurn(memory.Scope{Key: "test:sql-ordered", Kind: memory.ScopeKindPersonal},
		memory.ConversationRef{ConversationID: "conversation", AgentConversationID: "agent"},
		"turn", sqlIngestTime(), "Ada lives in Bishkek", "OK")
	if err != nil {
		t.Fatal(err)
	}

	return memory.SequencedTurn{ProducerID: "producer", ScopeSequence: 1, Fingerprint: "delivery", Turn: turn}
}

func sqlIngestBoundary(t *testing.T) memory.SequencedBoundary {
	t.Helper()
	turn := sqlIngestTurn(t)

	boundary, err := memory.NewBoundary(turn.Turn.Scope, turn.Turn.Conversation, "boundary",
		memory.BoundaryReasonClose, sqlIngestTime())
	if err != nil {
		t.Fatal(err)
	}

	return memory.SequencedBoundary{ProducerID: "producer", ScopeSequence: 1, Fingerprint: "delivery", Boundary: boundary}
}

func failSQLReceiptCursor(t *testing.T, fixture *storefixture.Fixture) func() {
	t.Helper()

	var exec func(string) error
	if fixture.Database.Postgres != nil {
		exec = func(statement string) error {
			_, err := fixture.Database.Postgres.Exec(t.Context(), statement)

			return err
		}
	} else {
		db, err := sql.Open("sqlite", fixture.Database.DSN)
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = db.Close() })

		exec = func(statement string) error {
			_, err := db.ExecContext(t.Context(), statement)

			return err
		}
	}

	var create, drop []string
	if fixture.Database.Postgres != nil {
		create = []string{"CREATE FUNCTION boyan_canonical.task10_receipt_fail() RETURNS trigger LANGUAGE plpgsql AS $$ " +
			"BEGIN RAISE EXCEPTION 'task10 receipt failure'; END $$",
			"CREATE TRIGGER task10_receipt_fail BEFORE INSERT ON boyan_canonical.ingest_cursor " +
				"FOR EACH ROW EXECUTE FUNCTION boyan_canonical.task10_receipt_fail()"}
		drop = []string{"DROP TRIGGER task10_receipt_fail ON boyan_canonical.ingest_cursor",
			"DROP FUNCTION boyan_canonical.task10_receipt_fail()"}
	} else {
		create = []string{"CREATE TRIGGER task10_receipt_fail BEFORE INSERT ON ingest_cursor " +
			"BEGIN SELECT RAISE(ABORT,'task10 receipt failure'); END"}
		drop = []string{"DROP TRIGGER task10_receipt_fail"}
	}

	for _, statement := range create {
		if err := exec(statement); err != nil {
			t.Fatal(err)
		}
	}

	return func() {
		for _, statement := range drop {
			if err := exec(statement); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func requireSQLCode(t *testing.T, err error, want memory.ErrorCode, class memory.ErrorClass) {
	t.Helper()

	code, gotClass, ok := memory.ClassifyError(err)
	if !ok || code != want || gotClass != class {
		t.Fatalf("error=%v code=%s class=%s want=%s/%s", err, code, gotClass, want, class)
	}
}
