package semantic_test

import (
	"context"
	"testing"
	"time"

	semanticstore "github.com/baldaworks/boyan/index/semantic"
)

func TestObservedMetadataRoundTripIsImmutableAndLegacyRemainsReadable(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "observed", true: "legacy"}[legacy], func(t *testing.T) {
			store, err := semanticstore.Open(t.TempDir())
			requireNoError(t, err)
			t.Cleanup(func() { requireNoError(t, store.Close()) })

			generation, coverage, chunks := fixture("observed-build", "lexical", 7, "a")
			observed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

			if !legacy {
				for i := range chunks {
					chunks[i].ObservedAt = &observed
				}
			}

			_, err = store.Stage(context.Background(), generation, coverage, chunks)
			requireNoError(t, err)
			requireNoError(t, store.Publish(context.Background(), semanticstore.Publication{
				View: generation.View(), BuildID: generation.BuildID,
			}))
			snapshot, found, err := store.Snapshot(context.Background(), generation.View())
			requireNoError(t, err)

			if !found {
				t.Fatal("complete snapshot unavailable")
			}

			got := snapshot.Chunks()
			if legacy {
				if got[0].ObservedAt != nil {
					t.Fatal("invented observed time for legacy row")
				}

				return
			}

			if got[0].ObservedAt == nil || !got[0].ObservedAt.Equal(observed) {
				t.Fatalf("observed roundtrip: %+v", got[0])
			}

			*got[0].ObservedAt = observed.Add(time.Hour)
			if !snapshot.Chunks()[0].ObservedAt.Equal(observed) {
				t.Fatal("snapshot observed time mutated through returned pointer")
			}
		})
	}
}
