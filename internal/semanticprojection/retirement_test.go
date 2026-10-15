package semanticprojection //nolint:testpackage // Exercises the actual manager publication path.

import (
	"context"
	"errors"
	"fmt"
	"testing"

	semanticstore "github.com/baldaworks/boyan/index/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

// Repair must reclaim previous vectors during refresh and forgetting, without
// relying on reopening the store or running startup garbage collection.
//
//nolint:cyclop,funlen // One lifecycle ties reclamation to refresh, forgetting, and a held public snapshot.
func TestRepairReclaimsRetiredGenerationsDuringRefreshAndForgetting(t *testing.T) {
	t.Parallel()

	scope := testScope("runtime-retirement")
	canonical := newFakeCanonical(scope, 1,
		testRecord(scope, "item", "revision-1", "first", memory.SensitivityStandard, memory.RetentionClassStandard),
	)
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "lex-1", 1)}
	store := openSemanticStore(t)
	manager := newTestManager(
		t, canonical, manifests, &fakeProvider{vector: []float32{1, 0}}, store, testModelSpace("model-a"),
	)

	var (
		previous semanticstore.State
		held     *semanticstore.Snapshot
	)

	for sequence := uint64(1); sequence <= 5; sequence++ {
		if sequence < 4 {
			canonical.replace(sequence, testRecord(
				scope, "item", fmt.Sprintf("revision-%d", sequence), "changed text",
				memory.SensitivityStandard, memory.RetentionClassStandard,
			))
		} else {
			// The canonical result of forgetting the only source, then the scope.
			canonical.replace(sequence)
		}

		manifests.set(activeManifest(scope, fmt.Sprintf("lex-%d", sequence), sequence))

		if err := manager.Repair(context.Background(), scope); err != nil {
			t.Fatalf("Repair(%d): %v", sequence, err)
		}

		current, found, err := store.ActiveState(context.Background(), scope)
		if err != nil || !found || current.LexicalWatermark != sequence {
			t.Fatalf("active state after Repair(%d): %#v, %t, %v", sequence, current, found, err)
		}

		if previous.BuildID != "" {
			err := store.Publish(context.Background(), semanticstore.Publication{
				View: previous.View(), BuildID: previous.BuildID,
				ExpectedActiveBuildID: current.BuildID, ExpectedActiveEpoch: current.PublicationEpoch,
			})
			if !errors.Is(err, semanticstore.ErrCorrupt) {
				t.Fatalf("old generation still stored after Repair(%d): %v", sequence, err)
			}
		}

		if sequence == 1 {
			held, found, err = store.Snapshot(context.Background(), current.View())
			if err != nil || !found {
				t.Fatalf("hold snapshot: %t, %v", found, err)
			}
		}

		if sequence >= 4 && current.ChunkCount != 0 {
			t.Fatalf("forgotten records retained in active projection: %#v", current)
		}

		previous = current
	}

	if got := held.Chunks(); len(got) != 1 || got[0].RevisionID != "revision-1" {
		t.Fatalf("held snapshot changed across refresh/forget: %#v", got)
	}
}
