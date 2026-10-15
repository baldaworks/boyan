//nolint:testpackage // Exercise legacy projection recovery through the real store and manager.
package semanticprojection

import (
	"context"
	"testing"
	"time"

	semanticstore "github.com/baldaworks/boyan/index/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

//nolint:cyclop // Full publication, readiness, and repair assertions exercise one legacy recovery contract.
func TestRepairUpgradesLegacyObservedMetadataWithoutEmbeddingAgain(t *testing.T) {
	ctx := context.Background()
	scope := testScope("legacy-observed")
	canonical := newFakeCanonical(scope, 7,
		testRecord(scope, "item-a", "revision-a", "memory", memory.SensitivityStandard, memory.RetentionClassStandard))
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "lexical", 7)}
	provider := &fakeProvider{vector: []float32{1, 0}}
	store := openSemanticStore(t)

	manager := newTestManager(t, canonical, manifests, provider, store, testModelSpace("model-a"))
	if err := manager.Repair(ctx, scope); err != nil {
		t.Fatal(err)
	}

	active, _, err := store.ActiveState(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}

	snapshot, _, err := store.Snapshot(ctx, active.View())
	if err != nil {
		t.Fatal(err)
	}

	chunks := snapshot.Chunks()
	chunks[0].BuildID = "legacy-build"
	chunks[0].ObservedAt = nil
	generation := semanticstore.Generation{Scope: scope, BuildID: "legacy-build", LexicalGenerationID: "lexical",
		LexicalWatermark: 7, ModelSpace: active.ModelSpace, Dimensions: 2, ReadyAt: time.Unix(100, 0).UTC()}

	coverage := []semanticstore.CoverageRecord{{ItemID: "item-a", RevisionID: "revision-a", Revision: 1,
		ContentHash: chunks[0].ContentHash, ChunkCount: 1}}
	if _, err := store.Stage(ctx, generation, coverage, chunks); err != nil {
		t.Fatal(err)
	}

	if err := store.Publish(ctx, semanticstore.Publication{View: generation.View(), BuildID: generation.BuildID,
		ExpectedActiveBuildID: active.BuildID, ExpectedActiveEpoch: active.PublicationEpoch}); err != nil {
		t.Fatal(err)
	}

	status, err := manager.Status(ctx, scope)
	if err != nil || status.State != StateDegraded {
		t.Fatalf("legacy temporal metadata advertised ready: %+v, %v", status, err)
	}

	before := len(provider.Inputs())

	if err := manager.Repair(ctx, scope); err != nil {
		t.Fatal(err)
	}

	snapshot, found, err := store.Snapshot(ctx, generation.View())
	if err != nil || !found || snapshot.Chunks()[0].ObservedAt == nil {
		t.Fatalf("legacy metadata not rebuilt: %v, %v", found, err)
	}

	if len(provider.Inputs()) != before {
		t.Fatal("metadata-only repair unnecessarily embedded existing vectors")
	}
}
