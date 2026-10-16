package semantic_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	semanticstore "github.com/baldaworks/boyan/index/semantic"
)

//nolint:cyclop // The legacy-to-observed publication lifecycle is one regression contract.
func TestPortableBuildReuseOwnsVectorsAndLegacyObservationReadiness(t *testing.T) {
	t.Parallel()

	store, err := semanticstore.Open(filepath.Join(t.TempDir(), "embeddings"))
	requireNoError(t, err)
	t.Cleanup(func() { requireNoError(t, store.Close()) })

	ctx := context.Background()
	generation, coverage, chunks := fixture("reuse", "lexical", 7, "a")
	_, err = store.Stage(ctx, generation, coverage, chunks)
	requireNoError(t, err)
	requireNoError(t, store.Publish(ctx, semanticstore.Publication{View: generation.View(), BuildID: generation.BuildID}))

	ready, err := store.ObservedMetadataReady(ctx, generation.View())
	if err != nil || ready {
		t.Fatalf("legacy readiness = %v, %v", ready, err)
	}

	reusable, found, err := store.LoadReusable(ctx, generation.View())
	if err != nil || !found || len(reusable.Chunks) != 2 || reusable.Chunks[0].ItemID != "item-a" {
		t.Fatalf("legacy reuse = %#v, %v, %v", reusable, found, err)
	}

	reusable.Chunks[0].Vector[0] = 0

	again, found, err := store.LoadReusable(ctx, generation.View())
	if err != nil || !found || again.Chunks[0].Vector[0] != 1 {
		t.Fatalf("reuse leaked ownership: %#v, %v, %v", again, found, err)
	}

	generation.BuildID = "observed"
	for index := range chunks {
		chunks[index].BuildID = generation.BuildID
		observed := generation.ReadyAt
		chunks[index].ObservedAt = &observed
	}

	_, err = store.Stage(ctx, generation, coverage, chunks)
	requireNoError(t, err)
	requireNoError(t, store.Publish(ctx, semanticstore.Publication{View: generation.View(), BuildID: generation.BuildID,
		ExpectedActiveBuildID: "reuse", ExpectedActiveEpoch: 1}))

	ready, err = store.ObservedMetadataReady(ctx, generation.View())
	if err != nil || !ready {
		t.Fatalf("rebuilt readiness = %v, %v", ready, err)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()

	if _, err := store.ObservedMetadataReady(canceled, generation.View()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled readiness = %v", err)
	}
}
