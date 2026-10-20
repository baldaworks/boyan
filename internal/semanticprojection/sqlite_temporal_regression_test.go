//go:build integration

package semanticprojection //nolint:testpackage // Retains audit timing through actual SQL and hydration.

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/baldaworks/boyan/internal/memoryread"
	"github.com/baldaworks/boyan/pkg/memory"
)

// Capturing AsOf after canonical dependency work or query inference loses the
// only eligible revision. SQL must supply the identity and canonical hydration
// must still release its text at the original RecallService instant.
//
//nolint:cyclop // Fixture and captured-time assertion remain in one simulated-time scope.
func TestSQLiteRecallPinsEligibilityBeforeSlowDependencies(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
		expires := start.Add(time.Second)
		f := newSQLiteManagerFixture(t, func(record *memory.RecallRecord) {
			record.CreatedAt = start.Add(-time.Hour)
			record.Temporal.ObservedAt = record.CreatedAt
			record.Temporal.ExpiresAt = &expires

			evidence, err := memory.NewEvidenceRef("source-a", "message", memory.MessageRoleUser,
				"eligible text", 0, 13, memory.AssertionModeUser)
			if err != nil {
				t.Fatal(err)
			}

			record.Evidence = []memory.EvidenceRef{evidence}
		})

		f.manager.now = func() time.Time { return start }
		if err := f.manager.Repair(t.Context(), f.scope); err != nil {
			t.Fatal(err)
		}

		h, err := NewHybrid(HybridDependencies{Canonical: f.canonical, Manifests: f.checkpoints,
			Reader: f.store, Lexical: f.store, Provider: &fakeProvider{vector: []float32{1, 0}},
			ModelSpace: testModelSpace("model-a")})
		if err != nil {
			t.Fatal(err)
		}

		h.provider = queryProviderFunc(func(context.Context, []string) ([][]float32, error) {
			time.Sleep(2 * time.Second)

			return [][]float32{{1, 0}}, nil
		})

		service, err := memoryread.NewRecallService(delayedRecallReader{f.canonical}, h)
		if err != nil {
			t.Fatal(err)
		}

		request := memory.RecallRequest{Scope: f.scope, Query: "eligible", Limit: 10}

		response, searchErr := service.Search(t.Context(), request)
		if searchErr != nil || len(response.Results) != 1 || response.Results[0].RevisionID != "revision-a" ||
			response.Retrieval == nil || response.Retrieval.Effective != "hybrid" {
			t.Fatalf("entry eligibility lost through SQL/hydration: %#v %v", response, searchErr)
		}

		if request.AsOf != nil {
			t.Fatal("recall mutated caller eligibility time")
		}
	})
}
