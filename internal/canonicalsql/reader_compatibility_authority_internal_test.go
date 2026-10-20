//go:build integration

package canonicalsql

import (
	"context"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestCompatibilityBenignSensitiveAggregate(t *testing.T) {
	for _, stage := range []string{"before-lookup", "before-publication"} {
		t.Run(stage, func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, reopen func() *Owner) {
				h := prepareCompatibilityHistory(t, owner)
				if stage == "before-publication" {
					assertSensitiveCompatibilityPublication(t, owner, h)
				} else {
					makeReaderAggregateSensitive(t, owner, h)
				}

				assertSensitiveCompatibilityLookup(t, owner, h)

				if err := owner.Close(); err != nil {
					t.Fatal(err)
				}

				assertSensitiveCompatibilityLookup(t, reopen(), h)
			})
		})
	}
}

func assertSensitiveCompatibilityPublication(t *testing.T, owner *Owner, h privacyHistory) {
	t.Helper()

	graph := traceGraph{captured: readerSnapshot{sources: make(map[string]memory.CanonicalSourceRecord),
		revisions: make(map[string]memory.MemoryRevision)}}

	var (
		payload memory.CanonicalCompatibilityPayload
		found   bool
	)

	if err := owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
		var err error

		payload, found, err = loadCompatibilitySnapshot(ctx, tx, h.scope, h.revision.RevisionID, &graph)

		return err
	}); err != nil || !found {
		t.Fatalf("initial compatibility found=%v error=%v", found, err)
	}

	makeReaderAggregateSensitive(t, owner, h)
	got, found, err := owner.publishCompatibility(t.Context(), graph, payload, found)
	assertBenignCompatibilityIDs(t, got, found, err)
}

func assertSensitiveCompatibilityLookup(t *testing.T, owner *Owner, h privacyHistory) {
	t.Helper()

	got, found, err := owner.LoadCanonicalCompatibility(t.Context(), h.scope, h.revision.RevisionID)
	assertBenignCompatibilityIDs(t, got, found, err)
}

func assertBenignCompatibilityIDs(t *testing.T, got memory.CanonicalCompatibilityPayload, found bool, err error) {
	t.Helper()

	if err != nil || !found || got.LegacyItemID != "legacy-item" || got.LegacyRevisionID != "legacy-revision" ||
		got.LegacyOperationID != "legacy-operation" || got.Text != "compatibility memory" {
		t.Fatalf("benign compatibility lost legacy identities: payload=%+v found=%v error=%v", got, found, err)
	}
}
