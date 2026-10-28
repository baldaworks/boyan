//go:build integration

package semanticprojection //nolint:testpackage // Exercises deterministic private lifecycle and race seams.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	semanticcore "github.com/baldaworks/boyan/internal/semantic"
	semanticstore "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

//nolint:cyclop,lll // One end-to-end assertion keeps privacy, publication, and status evidence together.
func TestRepairBuildsOnlyEligibleCurrentRecordsForExactLexicalView(t *testing.T) {
	t.Parallel()

	scope := testScope("eligible")
	canonical := newFakeCanonical(scope, 7,
		testRecord(scope, "item-a", "revision-a", "eligible text", memory.SensitivityStandard, memory.RetentionClassStandard),
		testRecord(scope, "item-b", "revision-b", "secret text", memory.SensitivitySensitive, memory.RetentionClassStandard),
		testRecord(scope, "item-c", "revision-c", "unsupported retention", memory.SensitivityStandard, memory.RetentionClass("unsupported")),
	)
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "sqlite-generation-7", 7)}
	provider := &fakeProvider{vector: []float32{1, 0}}
	store := openSemanticStore(t, canonical)
	space := testModelSpace("model-a")
	manager := newTestManager(t, canonical, manifests, provider, store, space)

	if err := manager.Repair(context.Background(), scope); err != nil {
		t.Fatalf("Repair() error = %v", err)
	}

	fingerprint, err := semanticcore.SpaceFingerprint(space)
	if err != nil {
		t.Fatalf("SpaceFingerprint() error = %v", err)
	}

	snapshot, found, err := store.LoadReusable(context.Background(), semanticstore.View{
		Scope: scope, LexicalGenerationID: "sqlite-generation-7", LexicalWatermark: 7,
		ModelSpace: fingerprint, Dimensions: 2,
	})
	if err != nil || !found {
		t.Fatalf("Snapshot() = (_, %t, %v), want ready snapshot", found, err)
	}

	chunks := snapshot.Chunks
	if len(chunks) != 1 || chunks[0].RevisionID != "revision-a" || storedChunkSequence(t, store, "revision-a") != 7 {
		t.Fatalf("Snapshot().Chunks() = %#v, want only eligible current revision", chunks)
	}

	inputs := provider.Inputs()
	if len(inputs) != 1 || inputs[0] != "passage: eligible text" {
		t.Fatalf("provider inputs = %#v, want eligible text only", inputs)
	}

	status, err := manager.Status(context.Background(), scope)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}

	if status.State != StateReady || status.SafeReason != "" || status.LexicalWatermark != 7 {
		t.Fatalf("Status() = %#v, want ready exact view", status)
	}
}

//nolint:wsl_v5 // The inactive fake hydration must be rejected before the provider boundary.
func TestRepairExcludesNonActiveHydrationBeforeProviderCall(t *testing.T) {
	t.Parallel()

	scope := testScope("inactive")
	record := testRecord(
		scope, "item-a", "revision-a", "superseded text",
		memory.SensitivityStandard, memory.RetentionClassStandard,
	)
	record.State = memory.RevisionStateSuperseded
	canonical := newFakeCanonical(scope, 7, record)
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "sqlite-generation-7", 7)}
	provider := &fakeProvider{vector: []float32{1, 0}}
	store := openSemanticStore(t, canonical)
	manager := newTestManager(t, canonical, manifests, provider, store, testModelSpace("model-a"))

	if err := manager.Repair(context.Background(), scope); err != nil {
		t.Fatalf("Repair() error = %v", err)
	}
	if inputs := provider.Inputs(); len(inputs) != 0 {
		t.Fatalf("provider inputs = %#v, want inactive record excluded", inputs)
	}

	state, found, err := store.ActiveState(context.Background(), scope)
	if err != nil || !found || state.Status != semanticstore.StatusReady || state.RecordCount != 0 {
		t.Fatalf("ActiveState() = (%#v, %t, %v), want complete empty state", state, found, err)
	}
}

//nolint:wsl_v5 // Cold state and provider inertness form one status contract.
func TestStatusClassifiesColdProjectionWithoutProviderOrCanonicalWork(t *testing.T) {
	t.Parallel()

	scope := testScope("cold")
	canonical := newFakeCanonical(scope, 7)
	provider := &fakeProvider{vector: []float32{1, 0}}
	manager := newTestManager(
		t,
		canonical,
		&fakeManifestSource{manifest: activeManifest(scope, "sqlite-generation-7", 7)},
		provider,
		openSemanticStore(t, canonical),
		testModelSpace("model-a"),
	)

	status, err := manager.Status(context.Background(), scope)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.State != StateDegraded || status.SafeReason != semanticcore.ReasonUnavailable ||
		status.ModelSpace == "" {
		t.Fatalf("Status() = %#v, want bounded cold degradation", status)
	}
	if inputs := provider.Inputs(); len(inputs) != 0 {
		t.Fatalf("cold Status() contacted provider with %#v", inputs)
	}
}

//nolint:cyclop,lll,wsl_v5 // The three successive generations prove the reuse boundary together.
func TestRepairReusesOnlySameModelSpaceAndPublishesCompleteReplacement(t *testing.T) {
	t.Parallel()

	scope := testScope("reuse")
	record := testRecord(scope, "item-a", "revision-a", "stable text", memory.SensitivityStandard, memory.RetentionClassStandard)
	canonical := newFakeCanonical(scope, 7, record)
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "sqlite-generation-7", 7)}
	store := openSemanticStore(t, canonical)

	firstProvider := &fakeProvider{vector: []float32{1, 0}}
	first := newTestManager(t, canonical, manifests, firstProvider, store, testModelSpace("model-a"))
	if err := first.Repair(context.Background(), scope); err != nil {
		t.Fatalf("first Repair() error = %v", err)
	}

	canonical.setSequence(8)
	manifests.set(activeManifest(scope, "sqlite-generation-8", 8))
	reuseProvider := &fakeProvider{vector: []float32{0, 1}}
	reuse := newTestManager(t, canonical, manifests, reuseProvider, store, testModelSpace("model-a"))
	if err := reuse.Repair(context.Background(), scope); err != nil {
		t.Fatalf("same-space Repair() error = %v", err)
	}
	if inputs := reuseProvider.Inputs(); len(inputs) != 0 {
		t.Fatalf("same-space provider inputs = %#v, want verified vector reuse", inputs)
	}

	canonical.setSequence(9)
	manifests.set(activeManifest(scope, "sqlite-generation-9", 9))
	changedProvider := &fakeProvider{vector: []float32{0, 1}}
	changedSpace := testModelSpace("model-b")
	changed := newTestManager(t, canonical, manifests, changedProvider, store, changedSpace)
	if err := changed.Repair(context.Background(), scope); err != nil {
		t.Fatalf("changed-space Repair() error = %v", err)
	}
	if inputs := changedProvider.Inputs(); len(inputs) != 1 {
		t.Fatalf("changed-space provider inputs = %#v, want forced inference", inputs)
	}

	fingerprint, err := semanticcore.SpaceFingerprint(changedSpace)
	if err != nil {
		t.Fatalf("SpaceFingerprint() error = %v", err)
	}
	snapshot, found, err := store.LoadReusable(context.Background(), semanticstore.View{
		Scope: scope, LexicalGenerationID: "sqlite-generation-9", LexicalWatermark: 9,
		ModelSpace: fingerprint, Dimensions: 2,
	})
	if err != nil || !found || len(snapshot.Chunks) != 1 || snapshot.Chunks[0].Vector[1] != 1 {
		t.Fatalf("changed-space Snapshot() = (%#v, %t, %v)", snapshot, found, err)
	}
}

//nolint:lll // The setup remains explicit for the publication-race invariant.
func TestRepairRejectsCanonicalAndManifestRaceBeforePublication(t *testing.T) {
	t.Parallel()

	scope := testScope("race")
	canonical := newFakeCanonical(scope, 7,
		testRecord(scope, "item-a", "revision-a", "eligible text", memory.SensitivityStandard, memory.RetentionClassStandard),
	)
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "sqlite-generation-7", 7)}
	store := openSemanticStore(t, canonical)
	manager := newTestManager(t, canonical, manifests, &fakeProvider{vector: []float32{1, 0}}, store, testModelSpace("model-a"))
	manager.beforeFinalCheck = func() {
		canonical.setSequence(8)
		manifests.set(activeManifest(scope, "sqlite-generation-8", 8))
	}

	err := manager.Repair(context.Background(), scope)
	if !errors.Is(err, ErrStaleView) {
		t.Fatalf("Repair() error = %v, want ErrStaleView", err)
	}

	if state, found, stateErr := store.ActiveState(context.Background(), scope); stateErr != nil || found {
		t.Fatalf("ActiveState() = (%#v, %t, %v), want no publication", state, found, stateErr)
	}
}

//nolint:lll,wsl_v5 // The before/after setup remains explicit for the privacy transition.
func TestRepairPrivacyTransitionReplacesOldReadyStateWithEmptyCompleteState(t *testing.T) {
	t.Parallel()

	scope := testScope("privacy")
	record := testRecord(scope, "item-a", "revision-a", "eligible text", memory.SensitivityStandard, memory.RetentionClassStandard)
	canonical := newFakeCanonical(scope, 7, record)
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "sqlite-generation-7", 7)}
	store := openSemanticStore(t, canonical)
	manager := newTestManager(t, canonical, manifests, &fakeProvider{vector: []float32{1, 0}}, store, testModelSpace("model-a"))
	if err := manager.Repair(context.Background(), scope); err != nil {
		t.Fatalf("initial Repair() error = %v", err)
	}

	record.Sensitivity = memory.SensitivitySensitive
	canonical.replace(8, record)
	manifests.set(activeManifest(scope, "sqlite-generation-8", 8))
	if err := manager.Repair(context.Background(), scope); err != nil {
		t.Fatalf("privacy Repair() error = %v", err)
	}

	fingerprint, err := semanticcore.SpaceFingerprint(testModelSpace("model-a"))
	if err != nil {
		t.Fatalf("SpaceFingerprint() error = %v", err)
	}
	snapshot, found, err := store.LoadReusable(context.Background(), semanticstore.View{
		Scope: scope, LexicalGenerationID: "sqlite-generation-8", LexicalWatermark: 8,
		ModelSpace: fingerprint, Dimensions: 2,
	})
	if err != nil || !found || len(snapshot.Chunks) != 0 || snapshot.State.RecordCount != 0 {
		t.Fatalf("privacy Snapshot() = (%#v, %t, %v), want complete empty replacement", snapshot, found, err)
	}
}

//nolint:cyclop,funlen,wsl_v5,lll // Queue boundary and coalescing assertions share one deterministic blocked-worker fixture.
func TestSchedulerUsesTwoWorkersCoalescesScopesAndBoundsQueueAt64(t *testing.T) {
	scope := testScope("scheduler")
	canonical := newFakeCanonical(scope, 7)
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "sqlite-generation-7", 7)}
	manager := newTestManager(t, canonical, manifests, &fakeProvider{vector: []float32{1, 0}}, openSemanticStore(t, canonical), testModelSpace("model-a"))

	started := make(chan memory.Scope, semanticcore.SemanticWorkerLimit)
	release := make(chan struct{})
	manager.beforeBuild = func(ctx context.Context, scope memory.Scope) {
		select {
		case started <- scope:
		case <-ctx.Done():
			return
		}
		select {
		case <-release:
		case <-ctx.Done():
		}
	}

	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	first := testScope("scheduled-000")
	second := testScope("scheduled-001")
	if err := manager.Schedule(first); err != nil {
		t.Fatalf("Schedule(first) error = %v", err)
	}
	if err := manager.Schedule(second); err != nil {
		t.Fatalf("Schedule(second) error = %v", err)
	}
	for range semanticcore.SemanticWorkerLimit {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("workers did not start")
		}
	}

	for index := 0; index < 100; index++ {
		if err := manager.Schedule(first); err != nil {
			t.Fatalf("coalesced Schedule() error = %v", err)
		}
	}

	for index := 0; index < semanticcore.SemanticQueueLimit; index++ {
		queued := testScope(fmt.Sprintf("queued-%03d", index))
		if err := manager.Schedule(queued); err != nil {
			t.Fatalf("Schedule(queue index %d) error = %v", index, err)
		}
	}

	overflow := testScope("queued-overflow")
	if err := manager.Schedule(overflow); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("Schedule(one over queue) error = %v, want ErrQueueFull", err)
	}
	status, err := manager.Status(context.Background(), overflow)
	if err != nil || status.State != StateDegraded || status.SafeReason != semanticcore.ReasonProjectionCapacity {
		t.Fatalf("overflow Status() = (%#v, %v)", status, err)
	}

	close(release)
}

//nolint:wsl_v5 // The full over-bound fixture proves rejection precedes provider inference.
func TestRepairRejectsOneOverChunkCapacityBeforeProviderCall(t *testing.T) {
	scope := testScope("capacity")
	records := make([]memory.RecallRecord, semanticcore.ProjectionChunkLimit+1)
	for index := range records {
		records[index] = testRecord(
			scope,
			fmt.Sprintf("item-%05d", index),
			fmt.Sprintf("revision-%05d", index),
			"bounded text",
			memory.SensitivityStandard,
			memory.RetentionClassStandard,
		)
	}
	canonical := newFakeCanonical(scope, 7, records...)
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "sqlite-generation-7", 7)}
	provider := &fakeProvider{vector: []float32{1, 0}}
	store := openSemanticStore(t, canonical)
	manager := newTestManager(t, canonical, manifests, provider, store, testModelSpace("model-a"))

	err := manager.Repair(context.Background(), scope)
	if !errors.Is(err, semanticstore.ErrCapacity) {
		t.Fatalf("Repair() error = %v, want semantic store capacity", err)
	}
	if inputs := provider.Inputs(); len(inputs) != 0 {
		t.Fatalf("provider received %d inputs before projection capacity rejection", len(inputs))
	}

	state, found, stateErr := store.ActiveState(context.Background(), scope)
	if stateErr != nil || !found || state.Status != semanticstore.StatusDegraded ||
		state.SafeReason != string(semanticcore.ReasonProjectionCapacity) {
		t.Fatalf("ActiveState() = (%#v, %t, %v), want capacity degradation", state, found, stateErr)
	}
}

//nolint:cyclop,wsl_v5 // Exact-capacity publication and every provider batch are one boundary contract.
func TestRepairAcceptsExactChunkCapacityInBoundedProviderBatches(t *testing.T) {
	scope := testScope("exact-capacity")
	records := make([]memory.RecallRecord, semanticcore.ProjectionChunkLimit)
	for index := range records {
		records[index] = testRecord(
			scope,
			fmt.Sprintf("item-%05d", index),
			fmt.Sprintf("revision-%05d", index),
			"bounded text",
			memory.SensitivityStandard,
			memory.RetentionClassStandard,
		)
	}
	canonical := newFakeCanonical(scope, 7, records...)
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "sqlite-generation-7", 7)}
	provider := &fakeProvider{vector: []float32{1, 0}}
	store := openSemanticStore(t, canonical)
	manager := newTestManager(t, canonical, manifests, provider, store, testModelSpace("model-a"))

	if err := manager.Repair(context.Background(), scope); err != nil {
		t.Fatalf("Repair(exact capacity) error = %v", err)
	}
	if inputs := provider.Inputs(); len(inputs) != semanticcore.ProjectionChunkLimit {
		t.Fatalf("provider input count = %d, want %d", len(inputs), semanticcore.ProjectionChunkLimit)
	}
	for _, size := range provider.BatchSizes() {
		if size < 1 || size > semanticcore.EmbeddingBatchLimit {
			t.Fatalf("provider batch size = %d, want 1..%d", size, semanticcore.EmbeddingBatchLimit)
		}
	}

	state, found, err := store.ActiveState(context.Background(), scope)
	if err != nil || !found || state.Status != semanticstore.StatusReady ||
		state.ChunkCount != semanticcore.ProjectionChunkLimit {
		t.Fatalf("ActiveState() = (%#v, %t, %v), want exact-capacity ready state", state, found, err)
	}
}

//nolint:wsl_v5 // Provider failure and durable marker assertions form one lifecycle contract.
func TestProviderFailurePublishesOnlyTextFreeDegradation(t *testing.T) {
	t.Parallel()

	scope := testScope("provider-failure")
	canonical := newFakeCanonical(scope, 7,
		testRecord(scope, "item-a", "revision-a", "never durable", memory.SensitivityStandard, memory.RetentionClassStandard),
	)
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "sqlite-generation-7", 7)}
	providerErr := &semanticcore.Error{Reason: semanticcore.ReasonUnavailable}
	manager := newTestManager(
		t, canonical, manifests, &fakeProvider{vector: []float32{1, 0}, err: providerErr},
		openSemanticStore(t, canonical), testModelSpace("model-a"),
	)

	if err := manager.Repair(context.Background(), scope); !errors.Is(err, providerErr) {
		t.Fatalf("Repair() error = %v, want provider failure", err)
	}
	state, found, err := manager.store.ActiveState(context.Background(), scope)
	if err != nil || !found || state.Status != semanticstore.StatusDegraded ||
		state.SafeReason != string(semanticcore.ReasonUnavailable) || state.ChunkCount != 0 || state.RecordCount != 0 {
		t.Fatalf("ActiveState() = (%#v, %t, %v), want text-free provider degradation", state, found, err)
	}
	_, ready, snapshotErr := manager.store.LoadReusable(context.Background(), state.View())
	if snapshotErr != nil || ready {
		t.Fatalf("Snapshot(degraded) = (_, %t, %v), want no readable partial state", ready, snapshotErr)
	}
}

//nolint:wsl_v5 // Startup, provider admission, and shutdown form one cancellation contract.
func TestShutdownCancelsInFlightProviderAndPublishesNothing(t *testing.T) {
	scope := testScope("shutdown")
	canonical := newFakeCanonical(scope, 7,
		testRecord(scope, "item-a", "revision-a", "cancel me", memory.SensitivityStandard, memory.RetentionClassStandard),
	)
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "sqlite-generation-7", 7)}
	provider := &cancelProvider{entered: make(chan struct{})}
	store := openSemanticStore(t, canonical)
	manager, err := New(Dependencies{
		Canonical: canonical, Records: canonical, Manifests: manifests,
		Physical: manifests,
		Provider: provider, Store: store, Lifecycle: store, ModelSpace: testModelSpace("model-a"),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := manager.Schedule(scope); err != nil {
		t.Fatalf("Schedule() error = %v", err)
	}

	select {
	case <-provider.entered:
	case <-time.After(time.Second):
		t.Fatal("provider was not entered")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := manager.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if state, found, stateErr := store.ActiveState(context.Background(), scope); stateErr != nil || found {
		t.Fatalf("ActiveState() = (%#v, %t, %v), want no canceled publication", state, found, stateErr)
	}
}

//nolint:wsl_v5 // Repair admission, close, and drain assertions form one shutdown contract.
func TestCloseCancelsAndDrainsExplicitRepair(t *testing.T) {
	scope := testScope("repair-shutdown")
	canonical := newFakeCanonical(scope, 7,
		testRecord(scope, "item-a", "revision-a", "cancel repair", memory.SensitivityStandard, memory.RetentionClassStandard),
	)
	provider := &cancelProvider{entered: make(chan struct{}), release: make(chan struct{})}
	store := openSemanticStore(t, canonical)
	manager, err := New(Dependencies{
		Canonical:  canonical,
		Records:    canonical,
		Manifests:  &fakeManifestSource{manifest: activeManifest(scope, "sqlite-generation-7", 7)},
		Physical:   &fakeManifestSource{manifest: activeManifest(scope, "sqlite-generation-7", 7)},
		Provider:   provider,
		Store:      store,
		Lifecycle:  store,
		ModelSpace: testModelSpace("model-a"),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	repairDone := make(chan error, 1)
	go func() { repairDone <- manager.Repair(context.Background(), scope) }()
	select {
	case <-provider.entered:
	case <-time.After(time.Second):
		t.Fatal("repair provider was not entered")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := manager.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case err := <-repairDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Repair() error = %v, want cancellation", err)
		}
	case <-time.After(100 * time.Millisecond):
		close(provider.release)
		t.Fatal("Close() returned before explicit repair drained")
	}
}

//nolint:wsl_v5 // Staging setup and post-start publication failure prove cleanup together.
func TestStartScrubsUnreachableStagingGeneration(t *testing.T) {
	scope := testScope("startup-cleanup")
	space := testModelSpace("model-a")
	fingerprint, err := semanticcore.SpaceFingerprint(space)
	if err != nil {
		t.Fatalf("SpaceFingerprint() error = %v", err)
	}
	canonical := newFakeCanonical(scope, 7, testRecord(scope, "item-a", "revision-a", "orphan content",
		memory.SensitivityStandard, memory.RetentionClassStandard))
	store := openSemanticStore(t, canonical)
	generation := semanticstore.Generation{
		Scope: scope, BuildID: "orphan-build", LexicalGenerationID: "sqlite-generation-7",
		LexicalWatermark: 7, ModelSpace: fingerprint, Dimensions: 2, ReadyAt: time.Now().UTC(),
	}
	contentHash := hashText("orphan content")
	inputHash := hashText("passage: orphan content")
	coverage := []semanticstore.CoverageRecord{{
		ItemID: "item-a", RevisionID: "revision-a", Revision: 1, ContentHash: contentHash, ChunkCount: 1,
	}}
	chunks := []semanticstore.ProjectionChunk{{
		Scope: scope, BuildID: generation.BuildID, LexicalGenerationID: generation.LexicalGenerationID,
		LexicalWatermark: 7, ModelSpace: fingerprint, Dimensions: 2,
		ItemID: "item-a", RevisionID: "revision-a", Revision: 1, Ordinal: 0,
		ContentHash: contentHash, InputHash: inputHash, ScopeChangeSeq: 7,
		Kind: memory.MemoryKindState, MemoryKey: "memory-item-a", CreatedAt: time.Unix(10, 0).UTC(),
		ObservedAt: &generation.ReadyAt, Vector: []float32{1, 0},
	}}
	if _, err := store.Stage(context.Background(), generation, coverage, chunks); err != nil {
		t.Fatalf("Stage(orphan) error = %v", err)
	}

	manager := newTestManager(
		t, canonical, &fakeManifestSource{manifest: activeManifest(scope, "sqlite-generation-7", 7)},
		&fakeProvider{vector: []float32{1, 0}}, store, space,
	)
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	err = store.Publish(context.Background(), semanticstore.Publication{
		View: generation.View(), BuildID: generation.BuildID,
	})
	if !errors.Is(err, semanticstore.ErrCorrupt) {
		t.Fatalf("Publish(scrubbed orphan) error = %v, want ErrCorrupt", err)
	}
}

//nolint:wsl_v5 // Construction and cleanup registration are one test-resource operation.
func newTestManager(
	t *testing.T,
	canonical *fakeCanonical,
	manifests *fakeManifestSource,
	provider *fakeProvider,
	store *neutralSemanticStore,
	space semanticcore.ModelSpace,
) *Manager {
	t.Helper()

	manager, err := New(Dependencies{
		Canonical:  canonical,
		Records:    canonical,
		Manifests:  manifests,
		Physical:   manifests,
		Provider:   provider,
		Store:      store,
		Lifecycle:  store,
		ModelSpace: space,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Errorf("Manager.Close() error = %v", err)
		}
	})

	return manager
}

func testScope(suffix string) memory.Scope {
	return memory.Scope{Key: "tenant:" + suffix, Kind: memory.ScopeKindPersonal}
}

func testModelSpace(model string) semanticcore.ModelSpace {
	return semanticcore.ModelSpace{
		Model: model, Revision: "immutable-revision", Dimensions: 2,
		QueryPrefix: "query: ", PassagePrefix: "passage: ",
	}
}

func activeManifest(scope memory.Scope, generation string, watermark uint64) memory.ProjectionManifest {
	return memory.ProjectionManifest{
		Scope: scope, ProjectionID: "sqlite", GenerationID: generation,
		Status: memory.ProjectionGenerationActive, Watermark: watermark, UpdatedAt: time.Unix(100, 0).UTC(),
	}
}

func testRecord(
	scope memory.Scope,
	itemID, revisionID, text string,
	sensitivity memory.Sensitivity,
	retention memory.RetentionClass,
) memory.RecallRecord {
	return memory.RecallRecord{
		Scope: scope, ItemID: itemID, RevisionID: revisionID, Revision: 1,
		Kind: memory.MemoryKindState, MemoryKey: memory.MemoryKey("memory-" + itemID),
		Text: text, State: memory.RevisionStateActive, CreatedAt: time.Unix(10, 0).UTC(),
		Temporal:    memory.Temporal{ObservedAt: time.Unix(10, 0).UTC()},
		Sensitivity: sensitivity, Retention: retention, ScopeChangeSeq: 7,
	}
}

//nolint:nlreturn // The helper directly returns the encoded digest.
func hashText(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

type cancelProvider struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

//nolint:wsl_v5 // Cancellation is the only completion path for this blocking provider.
func (provider *cancelProvider) Embed(ctx context.Context, _ []string) ([][]float32, error) {
	provider.once.Do(func() { close(provider.entered) })
	if provider.release == nil {
		<-ctx.Done()

		return nil, ctx.Err()
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-provider.release:
		return nil, errors.New("released provider")
	}
}

type fakeProvider struct {
	mu         sync.Mutex
	vector     []float32
	err        error
	inputs     []string
	batchSizes []int
}

//nolint:wsl_v5 // The fake records the complete external boundary before returning a configured outcome.
func (provider *fakeProvider) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.inputs = append(provider.inputs, inputs...)
	provider.batchSizes = append(provider.batchSizes, len(inputs))
	if provider.err != nil {
		return nil, provider.err
	}

	vectors := make([][]float32, len(inputs))
	for index := range inputs {
		vectors[index] = append([]float32(nil), provider.vector...)
	}

	return vectors, nil
}

func (provider *fakeProvider) BatchSizes() []int {
	provider.mu.Lock()
	defer provider.mu.Unlock()

	return append([]int(nil), provider.batchSizes...)
}

func (provider *fakeProvider) Inputs() []string {
	provider.mu.Lock()
	defer provider.mu.Unlock()

	return append([]string(nil), provider.inputs...)
}

type fakeManifestSource struct {
	mu               sync.Mutex
	manifest         memory.ProjectionManifest
	found            bool
	err              error
	physical         memory.ProjectionManifest
	physicalFound    bool
	physicalExplicit bool
}

func (source *fakeManifestSource) LoadActivePhysicalProjectionManifest(
	ctx context.Context,
	scope memory.Scope,
	projectionID string,
) (memory.ProjectionManifest, bool, error) {
	if err := ctx.Err(); err != nil {
		return memory.ProjectionManifest{}, false, err
	}

	source.mu.Lock()
	defer source.mu.Unlock()

	manifest := source.manifest
	found := source.found || manifest.GenerationID != ""

	if source.physicalExplicit {
		manifest = source.physical
		found = source.physicalFound
	}

	if found && (manifest.Scope != scope || manifest.ProjectionID != projectionID) {
		return memory.ProjectionManifest{}, false, errors.New("unexpected physical manifest request")
	}

	return manifest, found, source.err
}

//nolint:wsl_v5 // The fake validates the requested identity before returning its snapshot.
func (source *fakeManifestSource) LoadActiveProjectionManifest(
	ctx context.Context,
	scope memory.Scope,
	projectionID string,
) (memory.ProjectionManifest, bool, error) {
	if err := ctx.Err(); err != nil {
		return memory.ProjectionManifest{}, false, err
	}

	source.mu.Lock()
	defer source.mu.Unlock()
	if source.err != nil {
		return memory.ProjectionManifest{}, false, source.err
	}

	found := source.found
	if !found && source.manifest.GenerationID != "" {
		found = true
	}

	if found && (source.manifest.Scope != scope || source.manifest.ProjectionID != projectionID) {
		return memory.ProjectionManifest{}, false, errors.New("unexpected manifest request")
	}

	return source.manifest, found, nil
}

func (source *fakeManifestSource) set(manifest memory.ProjectionManifest) {
	source.mu.Lock()
	source.manifest = manifest
	source.found = true
	source.mu.Unlock()
}

func (source *fakeManifestSource) setPhysical(manifest memory.ProjectionManifest) {
	source.mu.Lock()
	source.physical = manifest
	source.physicalFound = manifest.GenerationID != ""
	source.physicalExplicit = true
	source.mu.Unlock()
}

type fakeCanonical struct {
	mu            sync.Mutex
	scope         memory.Scope
	sequence      uint64
	records       map[string]memory.RecallRecord
	active        []memory.ActiveCanonicalMemory
	loadStateHook func(context.Context)
	scanOverride  func(memory.ActiveMemoryScanRequest) []memory.ActiveCanonicalMemory
}

//nolint:wsl_v5 // Fixture construction is intentionally linear and explicit.
func newFakeCanonical(scope memory.Scope, sequence uint64, records ...memory.RecallRecord) *fakeCanonical {
	canonical := &fakeCanonical{scope: scope, sequence: sequence, records: make(map[string]memory.RecallRecord)}
	for _, record := range records {
		canonical.records[record.RevisionID] = record
		canonical.active = append(canonical.active, memory.ActiveCanonicalMemory{
			Item:       memory.MemoryItem{ItemID: record.ItemID, Scope: scope, Kind: record.Kind, MemoryKey: record.MemoryKey},
			RevisionID: record.RevisionID, Revision: record.Revision,
		})
	}
	sort.Slice(canonical.active, func(i, j int) bool {
		return activeItemAfter(canonical.active[j].Item.ItemID, canonical.active[i].Item.ItemID)
	})

	return canonical
}

//nolint:funcorder // Private race controls stay beside fixture construction.
func (canonical *fakeCanonical) setSequence(sequence uint64) {
	canonical.mu.Lock()
	canonical.sequence = sequence
	canonical.mu.Unlock()
}

//nolint:funcorder // Private race controls stay beside fixture construction.
func (canonical *fakeCanonical) replace(sequence uint64, records ...memory.RecallRecord) {
	replacement := newFakeCanonical(canonical.scope, sequence, records...)
	canonical.mu.Lock()
	canonical.sequence = replacement.sequence
	canonical.records = replacement.records
	canonical.active = replacement.active
	canonical.mu.Unlock()
}

//nolint:nlreturn,wsl_v5 // The fake directly returns its locked snapshot.
func (canonical *fakeCanonical) LoadScopeState(ctx context.Context, scope memory.Scope) (memory.ScopeState, error) {
	if canonical.loadStateHook != nil {
		canonical.loadStateHook(ctx)
	}
	if err := ctx.Err(); err != nil {
		return memory.ScopeState{}, err
	}

	canonical.mu.Lock()
	defer canonical.mu.Unlock()
	return memory.ScopeState{
		SchemaVersion: memory.CanonicalSchemaVersionV1,
		Scope:         scope,
		Version:       canonical.sequence,
		ChangeSeq:     canonical.sequence,
	}, nil
}

//nolint:wsl_v5 // The fake's deterministic cursor walk intentionally mirrors the storage contract.
func (canonical *fakeCanonical) ScanActiveMemory(
	ctx context.Context,
	request memory.ActiveMemoryScanRequest,
) ([]memory.ActiveCanonicalMemory, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	canonical.mu.Lock()
	defer canonical.mu.Unlock()
	if canonical.scanOverride != nil {
		return canonical.scanOverride(request), nil
	}
	result := make([]memory.ActiveCanonicalMemory, 0, request.Limit)
	for _, active := range canonical.active {
		if !activeItemAfter(active.Item.ItemID, request.AfterItemID) {
			continue
		}
		result = append(result, active)
		if uint32(len(result)) == request.Limit {
			break
		}
	}

	return result, nil
}

//nolint:wsl_v5 // The fake preserves request order like the canonical reader.
func (canonical *fakeCanonical) LoadRecallRecords(
	ctx context.Context,
	_ memory.Scope,
	revisionIDs []string,
) ([]memory.RecallRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	canonical.mu.Lock()
	defer canonical.mu.Unlock()
	result := make([]memory.RecallRecord, 0, len(revisionIDs))
	for _, id := range revisionIDs {
		if record, found := canonical.records[id]; found {
			record.ScopeChangeSeq = canonical.sequence
			result = append(result, record)
		}
	}

	return result, nil
}

//nolint:nlreturn // The adapter directly projects the state result.
func (canonical *fakeCanonical) CurrentScopeChangeSeq(ctx context.Context, scope memory.Scope) (uint64, error) {
	state, err := canonical.LoadScopeState(ctx, scope)
	return state.ChangeSeq, err
}

func (*fakeCanonical) SearchRecallTail(context.Context, memory.RecallRequest, uint32) ([]memory.RecallRecord, error) {
	return nil, nil
}

func (*fakeCanonical) ApplyCanonicalMutation(
	context.Context,
	memory.CanonicalMutation,
) (memory.CanonicalMutationOutcome, error) {
	panic("unexpected ApplyCanonicalMutation")
}

func (*fakeCanonical) ScanScopeChanges(context.Context, memory.Scope, uint64, uint32) ([]memory.ScopeChange, error) {
	panic("unexpected ScanScopeChanges")
}

func (*fakeCanonical) LoadCanonicalRevisions(
	context.Context,
	memory.CanonicalRevisionReadRequest,
) ([]memory.MemoryRevision, error) {
	panic("unexpected LoadCanonicalRevisions")
}

func (*fakeCanonical) ScanActiveHeads(context.Context, memory.ActiveHeadScanRequest) ([]memory.ItemHead, error) {
	panic("unexpected ScanActiveHeads")
}

func (*fakeCanonical) ClaimDeliveryOutbox(
	context.Context,
	memory.DeliveryClaimRequest,
) ([]memory.ClaimedDelivery, error) {
	panic("unexpected ClaimDeliveryOutbox")
}

func (*fakeCanonical) SettleDeliveryOutbox(context.Context, memory.DeliverySettlementRequest) error {
	panic("unexpected SettleDeliveryOutbox")
}
