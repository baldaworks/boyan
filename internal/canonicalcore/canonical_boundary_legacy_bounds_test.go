package canonicalcore_test

import (
	"context"

	"fmt"
	"testing"
	"time"

	core "github.com/baldaworks/boyan/internal/canonicalcore"
	"github.com/baldaworks/boyan/internal/canonicalsql"
	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestLegacyBoundaryBoundsResolveBeforeModelAdmission(t *testing.T) {
	t.Parallel()
	runCoreAuthorities(t, func(t *testing.T, factory coreFactory) {
		for _, mode := range []string{"inherited_expiry",
			"superseded_ancestor",
			"missing_parent",
			"cycle",
			"budget",
			"future_ancestor"} {
			t.Run(mode, func(t *testing.T) {
				t.Parallel()
				f := newInheritanceFixture(t, false, factory)
				outcome, err := f.processor.ProcessBoundary(context.Background(), f.export)
				requireNoError(t, err, "create fixture: %v")

				if mode == "superseded_ancestor" {
					supersedeLegacyAncestor(t, f)
				}

				unproveSQLLegacyBounds(t, f, outcome, mode)

				reader := f.reader

				view, err := reader.LoadCanonicalBoundaryViewAt(context.Background(), f.export.Scope, f.expiry)

				if legacyFixtureInvalid(mode) {
					want := m.CodeInvalidDerived
					if mode == "budget" {
						want = m.CodeLimitExceeded
					}

					if code, _, ok := m.ClassifyError(err); !ok || code != want {
						t.Errorf("legacy %s error=%v want %s", mode, err, want)
					}

					return
				}

				requireNoError(t, err, "resolve legacy bounds: %v")

				assertLegacyBoundsAdmission(t, f, reader, view, outcome)
			})
		}
	})
}

// Exercise hostile revision values directly after a complete valid native boundary.
func unproveSQLLegacyBounds(t *testing.T, f *inheritanceFixture, outcome m.BoundaryOutcome, mode string) {
	t.Helper()
	err := coreRawConnection(t, f.store).Update(t.Context(), func(tx *canonicalsql.Tx) error {
		rows, err := tx.Scan(
			t.Context(),
			canonicalsql.Range{Family: canonicalsql.Revision, Scope: &f.export.Scope, Limit: 32},
		)
		if err != nil {
			return err
		}

		for _, row := range rows {
			var revision m.MemoryRevision
			if err := canonicalsql.Decode(canonicalsql.Revision, row.Envelope, &revision); err != nil {
				return err
			}

			revision = unprovedLegacyRevision(
				revision,
				outcome.Scenarios.Revisions[0].RevisionID,
				outcome.Profile.Revisions[0].RevisionID,
				mode,
			)

			data, err := canonicalsql.Encode(canonicalsql.Revision, revision)
			if err != nil {
				return err
			}

			if err := tx.Put(t.Context(), row.Key, data); err != nil {
				return err
			}

			if mode == "budget" && revision.RevisionID == outcome.Scenarios.Revisions[0].RevisionID {
				if err := writeSQLLegacyBudget(t, tx, f.export.Scope, revision); err != nil {
					return err
				}
			}
		}

		return nil
	})
	requireNoError(t, err, "hostile legacy SQL revision fixture: %v")
}

func writeSQLLegacyBudget(t *testing.T, tx *canonicalsql.Tx, scope m.Scope, revision m.MemoryRevision) error {
	t.Helper()

	for n := range m.MaxSourcesPerRevision + 2 {
		ancestor := revision
		ancestor.RevisionID = fmt.Sprintf("legacy-budget-%04d", n)
		ancestor.Parents = []string{fmt.Sprintf("legacy-budget-%04d", n+1)}

		encoded, err := canonicalsql.Encode(canonicalsql.Revision, ancestor)
		if err != nil {
			return err
		}

		key := coreSQLKey(canonicalsql.Revision, scope, []byte(ancestor.RevisionID))
		if err := tx.Put(t.Context(), key, encoded); err != nil {
			return err
		}
	}

	return nil
}

func supersedeLegacyAncestor(t *testing.T, f *inheritanceFixture) {
	t.Helper()

	extractor := canonicalExtractorFunc(func(ctx context.Context,
		request m.CanonicalExtractionRequest) ([]m.SemanticCandidate,
		error) {
		candidates, err := extractTestStateCandidate(ctx, request)
		if err == nil {
			candidates[0].Predicate = "Parent B"
		}

		return candidates, err
	})
	processor, err := core.NewCanonicalTurnProcessor(f.store, extractor, m.PolicyRegistry{Version: "policy-v1"})
	requireNoError(t, err, "native processor: %v")
	turn,
		err := m.NewTurn(f.export.Scope,
		f.export.Conversation,
		"Parent B update",
		f.now.Add(time.Hour),
		"Parent B updated",
		"OK")
	requireNoError(t, err, "updated native: %v")
	_, err = processor.ProcessTurn(context.Background(), turn, testProcessorDerivation())
	requireNoError(t, err, "supersede native ancestor: %v")
}

func legacyFixtureInvalid(mode string) bool {
	return mode == "missing_parent" || mode == "cycle" || mode == "budget" || mode == "future_ancestor"
}

func assertLegacyBoundsAdmission(t *testing.T, f *inheritanceFixture, reader coreReader,
	view m.CanonicalBoundaryView, outcome m.BoundaryOutcome) {
	t.Helper()

	if len(view.Boundary.Eligible.Scenarios) != 0 || len(view.Boundary.Eligible.Profiles) != 0 {
		t.Fatal("legacy descendants outlived their actual ancestor in model context")
	}

	for _, ref := range []m.RevisionRef{outcome.Scenarios.Revisions[0], outcome.Profile.Revisions[0]} {
		parent := view.Boundary.Parents[ref]
		if parent.Temporal.ExpiresAt == nil || !parent.Temporal.ExpiresAt.Equal(f.expiry) {
			t.Errorf("legacy bound lost: %+v", parent)
		}
	}

	before, err := reader.LoadCanonicalBoundaryViewAt(context.Background(), f.export.Scope, f.expiry.Add(-time.Second))
	requireNoError(t, err, "before inherited expiry: %v")

	if len(before.Boundary.Eligible.Scenarios) != 1 || len(before.Boundary.Eligible.Profiles) != 1 {
		t.Fatal("superseded historical ancestor incorrectly excluded eligible descendant")
	}
}

func unprovedLegacyRevision(revision m.MemoryRevision, scenario, profile, mode string) m.MemoryRevision {
	if revision.GroundingBoundsVersion == 0 {
		if mode == "future_ancestor" {
			revision.Temporal.ObservedAt = time.Date(2026, time.October, 14, 0, 0, 0, 0, time.UTC)
		}

		return revision
	}

	revision.GroundingBoundsVersion = 0
	revision.GroundingParents = nil
	revision.Temporal.ValidFrom = nil
	revision.Temporal.ValidUntil = nil

	revision.Temporal.ExpiresAt = nil
	if revision.RevisionID == scenario {
		switch mode {
		case "missing_parent":
			revision.Parents = []string{"unavailable-parent"}
		case "cycle":
			revision.Parents = []string{profile}
		case "budget":
			revision.Parents = []string{"legacy-budget-0000"}
		}
	}

	return revision
}
