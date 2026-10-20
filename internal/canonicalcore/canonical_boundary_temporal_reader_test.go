package canonicalcore_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	core "github.com/baldaworks/boyan/internal/canonicalcore"
	forget "github.com/baldaworks/boyan/internal/forgetops"
	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestCanonicalBoundaryTemporalReaderSeparatesFactsFromHeads(t *testing.T) {
	t.Parallel()
	runCoreAuthorities(t, func(t *testing.T, factory coreFactory) {
		at := time.Date(2026, time.October, 13, 12, 0, 0, 0, time.UTC)

		before, after := at.Add(-time.Hour), at.Add(time.Hour)
		for _, test := range []struct {
			name     string
			temporal m.Temporal
			eligible int
		}{
			{name: "expiry_equality", temporal: m.Temporal{ObservedAt: before, ExpiresAt: &at}},
			{name: "validity_equality", temporal: m.Temporal{ObservedAt: before, ValidUntil: &at}},
			{name: "future_observation", temporal: m.Temporal{ObservedAt: after}},
			{name: "future_validity", temporal: m.Temporal{ObservedAt: before, ValidFrom: &after}},
			{name: "dated_event", temporal: m.Temporal{ObservedAt: before, EventAt: &before,
				TemporalText: "yesterday", TemporalPrecision: "day", Timezone: "Asia/Bishkek"}, eligible: 1},
			{name: "planned_event", temporal: m.Temporal{ObservedAt: at, EventAt: &after}, eligible: 1},
		} {
			t.Run(test.name, func(t *testing.T) {
				view := temporalReaderFixture(t, test.temporal, at, factory)
				if len(view.Snapshot.Atoms) != 1 || len(view.Boundary.Structural.Atoms) != 1 ||
					len(view.Boundary.Eligible.Atoms) != test.eligible || len(view.Boundary.Parents) != 1 {
					t.Fatalf("structural/eligible/metadata separation failed: %+v", view)
				}

				if test.eligible == 1 && !reflect.DeepEqual(view.Boundary.Eligible.Atoms[0].Temporal, &test.temporal) {
					t.Fatal("model context lost descriptive temporal metadata")
				}

				assertTemporalReaderMetadata(t, view, at)
			})
		}
	})
}

func temporalReaderFixture(
	t *testing.T,
	temporal m.Temporal,
	at time.Time,
	factory coreFactory,
) m.CanonicalBoundaryView {
	t.Helper()
	store, reader := newModelRetryStore(t, factory)
	turn := modelRetryTurn(t)
	extractor := canonicalExtractorFunc(func(ctx context.Context,
		request m.CanonicalExtractionRequest) ([]m.SemanticCandidate, error) {
		candidates, err := extractTestStateCandidate(ctx, request)
		if err == nil {
			candidates[0].Memory.Temporal = temporal
		}

		return candidates, err
	})
	processor, err := core.NewCanonicalTurnProcessor(store, extractor, m.PolicyRegistry{Version: "policy-v1"})
	requireNoError(t, err, "processor: %v")
	_, err = processor.ProcessTurn(context.Background(), turn, testProcessorDerivation())
	requireNoError(t, err, "native turn: %v")
	view, err := reader.LoadCanonicalBoundaryViewAt(context.Background(), turn.Scope, at)
	requireNoError(t, err, "temporal boundary view: %v")

	return view
}

func assertTemporalReaderMetadata(t *testing.T, view m.CanonicalBoundaryView, at time.Time) {
	t.Helper()
	requireNoError(t, view.Boundary.Validate(m.MaxSnapshotItems, at), "valid snapshot: %v")

	for ref, parent := range view.Boundary.Parents {
		if parent.CanonicalRef != view.LegacyToCanonical[ref] {
			t.Fatal("canonical temporal metadata resolved to a different exact revision")
		}

		missing := view.Boundary

		missing.Parents = map[m.RevisionRef]m.BoundaryParentMetadata{}
		if missing.Validate(m.MaxSnapshotItems, at) == nil {
			t.Fatal("missing canonical metadata was treated as unrestricted")
		}

		parent.Temporal.ObservedAt = time.Time{}
		invalid := view.Boundary

		invalid.Parents = map[m.RevisionRef]m.BoundaryParentMetadata{ref: parent}
		if invalid.Validate(m.MaxSnapshotItems, at) == nil {
			t.Fatal("missing native observation was accepted")
		}
	}
}

func TestBoundarySnapshotRejectsWrongScopeAndInactiveFacts(t *testing.T) {
	t.Parallel()
	runCoreAuthorities(t, func(t *testing.T, factory coreFactory) {
		at := time.Date(2026, time.October, 13, 12, 0, 0, 0, time.UTC)
		view := temporalReaderFixture(t, m.Temporal{ObservedAt: at}, at, factory)
		wrongScope := view.Boundary

		wrongScope.Eligible.Scope.Key = "test:other"
		if wrongScope.Validate(m.MaxSnapshotItems, at) == nil {
			t.Fatal("cross-scope factual view was accepted")
		}

		for _, state := range []m.RevisionState{m.RevisionStateSuperseded, m.RevisionStateInvalidated} {
			inactive := view.Boundary
			inactive.Eligible.Atoms = append([]m.Atom(nil), inactive.Eligible.Atoms...)

			inactive.Eligible.Atoms[0].Meta.State = state
			if inactive.Validate(m.MaxSnapshotItems, at) == nil {
				t.Fatalf("inactive factual head admitted: %s", state)
			}
		}
	})
}

func TestCanonicalBoundaryTemporalReaderOmitsForgottenHeads(t *testing.T) {
	t.Parallel()
	runCoreAuthorities(t, func(t *testing.T, factory coreFactory) {
		reader, _, turn, _, store := nativeFlow(t, factory)
		adapter, err := forget.NewCanonicalForgetAdapter(store)
		requireNoError(t, err, "forget adapter: %v")
		_, err = adapter.ForgetScope(context.Background(), m.ForgetScopeCommand{
			SchemaVersion: m.DerivedSchemaVersionV1, Scope: turn.Scope,
			RequestID: "temporal-context-forget", ForgottenAt: time.Now().UTC(),
		})
		requireNoError(t, err, "forget: %v")
		view, err := reader.LoadCanonicalBoundaryViewAt(context.Background(), turn.Scope, time.Now().UTC())
		requireNoError(t, err, "view after forgetting: %v")

		if len(view.Boundary.Structural.Atoms) != 0 || len(view.Boundary.Eligible.Atoms) != 0 ||
			len(view.Boundary.Parents) != 0 {
			t.Fatal("forgotten revision survived in boundary context")
		}
	})
}
