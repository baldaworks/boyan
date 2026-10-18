package storefixture_test

import (
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"strings"
	"sync"
	"testing"
)

func TestPublicBoundaryCapabilityInitializationRaces(t *testing.T) {
	for _, kind := range []string{"two-certified-stages", "unproven-writer"} {
		t.Run(kind, func(t *testing.T) {
			forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, _ mutationProbe) {
				first := nativeMutation(t)
				first.BoundaryIdentity = mutationBoundary(t, first.Scope)
				second := first

				second.Operation.OperationID = "second-stage"
				if kind == "unproven-writer" {
					second.BoundaryIdentity = nil
					second.Operation.OperationID = "boyan-derived:v1:operation:" + strings.Repeat("a", 64)
				}

				var group sync.WaitGroup

				results := make(chan error, 2)

				for _, mutation := range []memory.CanonicalMutation{first, second} {
					group.Go(func() { _, err := writeBoundary(t, store, mutation, true); results <- err })
				}

				group.Wait()
				close(results)

				wins := 0

				for err := range results {
					if err == nil {
						wins++
					} else {
						assertMutationCode(t, err, memory.CodeConflict)
					}
				}

				if wins != 1 {
					t.Fatalf("capability race winners=%d", wins)
				}

				assertMutationState(t, store, first.Scope, 1)
			})
		})
	}
}
