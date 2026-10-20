//go:build integration

package standaloneruntime //nolint:testpackage // Uses the production pipeline with deterministic model outputs.

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/boundarycore"
	"github.com/baldaworks/boyan/internal/canonicalcore"
	"github.com/baldaworks/boyan/internal/canonicalops"
	"github.com/baldaworks/boyan/internal/orderedingest"
	m "github.com/baldaworks/boyan/pkg/memory"
)

type sqlBoundaryModel struct{ calls int }

func (model *sqlBoundaryModel) SynthesizeScenarios(
	_ context.Context, request m.ScenarioSynthesisRequest,
) ([]m.ScenarioCandidate, error) {
	model.calls++

	if len(request.View.Atoms) != 1 {
		return nil, fmt.Errorf("expected one real canonical atom, got %d", len(request.View.Atoms))
	}

	atom := request.View.Atoms[0]
	candidate := m.ScenarioCandidate{TopicKey: "home", Title: "Home", Summary: "Bishkek home",
		Atoms: []m.RevisionRef{{ItemID: atom.Meta.ItemID, RevisionID: atom.Meta.RevisionID}}}

	if len(request.View.Scenarios) != 0 {
		previous := request.View.Scenarios[0]
		candidate.Supersedes = &m.RevisionRef{ItemID: previous.Meta.ItemID, RevisionID: previous.Meta.RevisionID}
	}

	return []m.ScenarioCandidate{candidate}, nil
}

func (model *sqlBoundaryModel) SynthesizeProfile(
	_ context.Context, request m.ProfileSynthesisRequest,
) (*m.ProfileCandidate, error) {
	model.calls++

	if len(request.View.Scenarios) != 1 {
		return nil, fmt.Errorf("expected one persisted scenario, got %d", len(request.View.Scenarios))
	}

	scenario := request.View.Scenarios[0]
	candidate := &m.ProfileCandidate{Disposition: m.ProfileDispositionUpsert, Summary: "Bishkek resident",
		Scenarios: []m.RevisionRef{{ItemID: scenario.Meta.ItemID, RevisionID: scenario.Meta.RevisionID}}}

	if len(request.View.Profiles) != 0 {
		previous := request.View.Profiles[0]
		candidate.Supersedes = &m.RevisionRef{ItemID: previous.Meta.ItemID, RevisionID: previous.Meta.RevisionID}
	}

	return candidate, nil
}

// Replace only external model work. Store, reader, boundary adapter, native
// processors, ordered receipts and actual local projection are production code.
func configureSQLRuntimePipeline(
	t *testing.T, runtime *Runtime,
) (*canonicalops.CanonicalBoundaryStore, *sqlBoundaryModel) {
	t.Helper()

	s := runtime.service
	requireNoError(t, s.sequenced.Close(t.Context()), "close unused model pipeline: %v")
	turn, err := canonicalcore.NewCanonicalTurnProcessor(s.store, runtimeFixtureExtractor{},
		m.PolicyRegistry{Version: "policy-v1"})
	requireNoError(t, err, "native processor: %v")
	adapter, err := canonicalops.NewCanonicalBoundaryStore(s.store, s.reader)
	requireNoError(t, err, "actual boundary adapter: %v")

	model := &sqlBoundaryModel{}
	boundary, err := boundarycore.NewBoundaryProcessor(adapter, model, model, m.LegacyDerivationRef())
	requireNoError(t, err, "boundary processor: %v")
	s.sequenced, err = orderedingest.NewSequencedIngestRuntime(s.store, s.store, turn, m.LegacyDerivationRef(),
		s.projection, boundary, s.projection, orderedingest.SequencedIngestConfig{})
	requireNoError(t, err, "ordered pipeline: %v")

	return adapter, model
}

func sqlRuntimeTurn(t *testing.T, scope m.Scope) m.Turn {
	t.Helper()

	conversation := m.ConversationRef{ConversationID: "native-conversation", AgentConversationID: "native-agent"}
	turn, err := m.NewTurn(scope, conversation,
		"native-turn", time.Now().UTC().Add(-time.Hour), "Bishkek home", "OK")
	requireNoError(t, err, "turn: %v")

	return turn
}

//nolint:cyclop,funlen // One restart trace preserves graph, receipt, replay and denial assertions together.
func TestSQLRuntimeNativeTurnBoundaryRestart(t *testing.T) {
	forEachSQLRuntimeConfig(t, func(t *testing.T, cfg Config) {
		runtime, err := New(cfg)
		requireNoError(t, err, "new: %v")
		t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
		_, model := configureSQLRuntimePipeline(t, runtime)
		requireNoError(t, runtime.Start(t.Context()), "start: %v")
		// Maximum raw scope bytes survive every receipt and SQL partition lookup.
		scope := m.Scope{Key: strings.Repeat("scope:/", 9362) + "x", Kind: m.ScopeKindPersonal}
		turn := sqlRuntimeTurn(t, scope)
		request := m.SequencedTurn{ProducerID: strings.Repeat("producer:", 1800), ScopeSequence: 1,
			Fingerprint: "turn-fingerprint", Turn: turn}
		turnOutcome, err := runtime.IngestSequencedTurn(t.Context(), request)
		requireNoError(t, err, "native ordered turn: %v")
		boundary, err := m.NewBoundary(scope, turn.Conversation, "native-close", m.BoundaryReasonClose, time.Now().UTC())
		requireNoError(t, err, "boundary: %v")

		boundaryRequest := m.SequencedBoundary{ProducerID: request.ProducerID, ScopeSequence: 2,
			Fingerprint: "boundary-fingerprint", Boundary: boundary}
		outcome, err := runtime.ApplySequencedBoundary(t.Context(), boundaryRequest)
		requireNoError(t, err, "nonempty boundary: %v")

		if model.calls != 2 || outcome.ScopeVersion <= turnOutcome.ScopeVersion || len(outcome.OperationIDs) != 2 {
			t.Fatalf("nonempty generation missing: calls=%d outcome=%+v", model.calls, outcome)
		}

		before := assertSQLRuntimeGraph(t, runtime, turn, outcome)
		receipt, found, err := runtime.service.store.LoadIngestSequence(t.Context(), request.ProducerID, scope, 2)
		requireNoError(t, err, "receipt: %v")

		if !found {
			t.Fatal("boundary receipt missing")
		}

		requireNoError(t, runtime.Stop(t.Context()), "stop: %v")
		runtime, err = New(cfg)
		requireNoError(t, err, "reopen: %v")
		_, model = configureSQLRuntimePipeline(t, runtime)
		requireNoError(t, runtime.Start(t.Context()), "restart: %v")

		after := assertSQLRuntimeGraph(t, runtime, turn, outcome)
		if !reflect.DeepEqual(before, after) {
			t.Fatal("canonical identities or source graph changed on restart")
		}

		_, err = runtime.IngestSequencedTurn(t.Context(), request)
		requireNoError(t, err, "turn replay: %v")
		_, err = runtime.ApplySequencedBoundary(t.Context(), boundaryRequest)
		requireNoError(t, err, "boundary replay: %v")
		replayed, found, err := runtime.service.store.LoadIngestSequence(t.Context(), request.ProducerID, scope, 2)
		requireNoError(t, err, "replayed receipt: %v")

		if !found || !reflect.DeepEqual(receipt, replayed) || model.calls != 0 {
			t.Fatal("replay changed receipt or reran synthesis")
		}

		boundaryRequest.Fingerprint = "changed"
		if _, err := runtime.ApplySequencedBoundary(t.Context(), boundaryRequest); err == nil {
			t.Fatal("altered replay accepted")
		}

		command := m.ForgetSourceCommand{SchemaVersion: m.DerivedSchemaVersionV1,
			Source: m.SourceRef{Scope: scope, ExportID: turn.ExportID,
				ConversationID: turn.Conversation.ConversationID, SourceTurnID: turn.SourceTurnID},
			ForgottenAt: time.Now().UTC()}
		forgotten, err := runtime.ForgetSource(t.Context(), command)
		requireNoError(t, err, "forget native source and retire actual projection: %v")
		requireNoError(t, runtime.Stop(t.Context()), "stop denied runtime: %v")
		runtime, err = New(cfg)
		requireNoError(t, err, "reopen denied authority: %v")
		requireNoError(t, runtime.Start(t.Context()), "restart denied authority: %v")
		denied, err := runtime.service.store.IsSourceDenied(t.Context(), scope, turn.SourceID)
		requireNoError(t, err, "persisted denial: %v")

		if !denied {
			t.Fatal("maximum compound source denial lost at restart")
		}

		for _, revision := range before {
			denied, err = runtime.service.store.IsRevisionDenied(t.Context(), scope, revision.RevisionID)
			requireNoError(t, err, "persisted revision denial: %v")

			if !denied {
				t.Fatal("derived revision denial lost at restart")
			}
		}

		replayedForget, err := runtime.ForgetSource(t.Context(), command)
		requireNoError(t, err, "forget replay completes retirement: %v")

		if !reflect.DeepEqual(forgotten, replayedForget) {
			t.Fatal("restart changed forget identity or version")
		}
	})
}

//nolint:cyclop // Verifies each independently required durable identity and actual projection effect.
func assertSQLRuntimeGraph(t *testing.T, runtime *Runtime, turn m.Turn, outcome m.IngestOutcome) []m.MemoryRevision {
	t.Helper()

	var revisions []m.MemoryRevision

	for _, id := range outcome.OperationIDs {
		op, found, err := runtime.service.store.LoadCanonicalOperation(t.Context(), turn.Scope, id)
		requireNoError(t, err, "operation: %v")

		if !found || len(op.Outcome.RevisionIDs) != 1 {
			t.Fatal("boundary stage did not persist a revision")
		}

		rows, err := runtime.service.store.LoadCanonicalRevisions(t.Context(), m.CanonicalRevisionReadRequest{
			Scope: turn.Scope, RevisionIDs: op.Outcome.RevisionIDs})
		requireNoError(t, err, "revisions: %v")

		if len(rows) != 1 || len(rows[0].Parents) == 0 ||
			len(rows[0].Evidence) == 0 || rows[0].Evidence[0].SourceID != turn.SourceID {
			t.Fatal("boundary lost canonical parent or source association")
		}

		revisions = append(revisions, rows...)
	}

	proof, err := runtime.service.store.VerifyCanonicalTurn(t.Context(), turn)
	requireNoError(t, err, "turn proof: %v")

	if !proof.Found || proof.Forgotten || len(proof.Messages) != 2 {
		t.Fatal("native identity changed")
	}

	// These native processors synchronously project and do not enqueue delivery
	// records. Verify that restart/replay cannot fabricate asynchronous effects.
	now := time.Now().UTC()
	delivery, err := runtime.service.store.ClaimDeliveryOutbox(t.Context(), m.DeliveryClaimRequest{
		Scope: turn.Scope, LeaseOwner: "runtime-outbox-check", Now: now, LeaseUntil: now.Add(time.Minute), Limit: 1})
	requireNoError(t, err, "native outbox: %v")

	if len(delivery) != 0 {
		t.Fatal("native pipeline fabricated an asynchronous delivery")
	}

	manifest, found, err := runtime.service.store.LoadActiveProjectionManifest(t.Context(), turn.Scope, boyanProjectionID)
	requireNoError(t, err, "manifest: %v")

	if !found || manifest.Watermark != outcome.ScopeChangeSeq {
		t.Fatal("actual projection did not reach boundary commit")
	}

	db := openRuntimeSQL(t, runtime)
	defer db.Close()

	var count int

	err = db.QueryRowContext(t.Context(), "SELECT count(*) FROM lexical_record").Scan(&count)
	requireNoError(t, err, "actual nonempty generation: %v")

	if count < 3 {
		t.Fatalf("native atom/scenario/profile projection rows=%d", count)
	}

	return revisions
}
