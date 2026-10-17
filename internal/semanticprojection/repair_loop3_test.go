package semanticprojection //nolint:testpackage // Exercises deterministic private lifecycle and race seams.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	semanticstore "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

//nolint:cyclop,wsl_v5 // The exact W7 flight and W8 request ordering is the regression contract.
func TestScheduleAfterOlderExplicitRepairSnapshotRerunsNewestCertifiedView(t *testing.T) {
	scope := testScope("schedule-after-repair-snapshot")
	record := testRecord(scope, "item-a", "revision-a", "text", memory.SensitivityStandard, memory.RetentionClassStandard)
	canonical := newFakeCanonical(scope, 7, record)
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "sqlite-generation-7", 7)}
	provider := newGenerationBlockingProvider()
	store := openSemanticStore(t, canonical)
	manager, err := New(Dependencies{
		Canonical: canonical, Records: canonical, Manifests: manifests, Physical: manifests,
		Provider: provider, Store: store, Lifecycle: store, ModelSpace: testModelSpace("model-a"),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if closeErr := manager.Close(ctx); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	joined := make(chan struct{})
	var joinedOnce sync.Once
	manager.joinedFlight = func() { joinedOnce.Do(func() { close(joined) }) }
	repairDone := make(chan error, 1)
	go func() { repairDone <- manager.Repair(context.Background(), scope) }()

	waitClosed(t, provider.firstEntered, "W7 provider call was not entered")
	canonical.replace(8, record)
	manifests.set(activeManifest(scope, "sqlite-generation-8", 8))
	if err := manager.Schedule(scope); err != nil {
		t.Fatalf("Schedule(W8) error = %v", err)
	}
	waitClosed(t, joined, "scheduled W8 request did not join W7 flight")
	for range 100 {
		if err := manager.Schedule(scope); err != nil {
			t.Fatalf("coalesced Schedule(W8) error = %v", err)
		}
	}
	close(provider.releaseFirst)

	if err := <-repairDone; !errors.Is(err, ErrStaleView) {
		t.Fatalf("Repair(W7) error = %v, want stale view", err)
	}
	waitClosed(t, provider.secondEntered, "scheduled W8 request was lost after W7 flight completed")

	state := waitActiveState(t, store, scope, func(state semanticstore.State, found bool) bool {
		return found && state.Status == semanticstore.StatusReady &&
			state.LexicalGenerationID == "sqlite-generation-8" && state.LexicalWatermark == 8
	})
	if provider.Calls() != 2 {
		t.Fatalf("provider calls = %d, want exactly W7 plus one coalesced W8 rerun", provider.Calls())
	}
	if state.Status != semanticstore.StatusReady {
		t.Fatalf("ActiveState() = %#v, want W8 ready", state)
	}
}

//nolint:wsl_v5 // Cancellation and status reconciliation are one observable lifecycle contract.
func TestCanceledRepairOverCompatibleReadyProjectionStillReportsReady(t *testing.T) {
	scope := testScope("cancel-over-ready")
	record := testRecord(scope, "item-a", "revision-a", "text", memory.SensitivityStandard, memory.RetentionClassStandard)
	canonical := newFakeCanonical(scope, 7, record)
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "sqlite-generation-7", 7)}
	manager := newTestManager(
		t, canonical, manifests, &fakeProvider{vector: []float32{1, 0}},
		openSemanticStore(t, canonical), testModelSpace("model-a"),
	)
	if err := manager.Repair(context.Background(), scope); err != nil {
		t.Fatalf("initial Repair() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := manager.Repair(ctx, scope); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Repair() error = %v, want context cancellation", err)
	}

	status, err := manager.Status(context.Background(), scope)
	if err != nil || status.State != StateReady || status.LexicalWatermark != 7 {
		t.Fatalf("Status() = (%#v, %v), want reconciled W7 ready", status, err)
	}
}

//nolint:cyclop,lll // State, exact owner and epoch prove the real verify-to-publish gap.
func TestVerifyPublishGapRacePublishesThenInvalidatesStaleGeneration(t *testing.T) {
	scope := testScope("verify-publish-gap")
	record := testRecord(scope, "item-a", "revision-a", "text", memory.SensitivityStandard, memory.RetentionClassStandard)
	canonical := newFakeCanonical(scope, 7, record)
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "sqlite-generation-7", 7)}
	store := openSemanticStore(t, canonical)
	manager := newTestManager(t, canonical, manifests, &fakeProvider{vector: []float32{1, 0}}, store, testModelSpace("model-a"))
	manager.beforePublish = func() {
		canonical.replace(8, record)
		manifests.set(activeManifest(scope, "sqlite-generation-8", 8))
	}

	var published semanticstore.State

	manager.afterPublish = func() {
		var found bool

		var err error

		published, found, err = store.ActiveState(context.Background(), scope)
		if err != nil || !found || published.Status != semanticstore.StatusReady || published.PublicationEpoch != 1 {
			t.Fatalf("missing original publication before exact invalidation: %#v %t %v", published, found, err)
		}
	}

	if err := manager.Repair(context.Background(), scope); !errors.Is(err, ErrStaleView) {
		t.Fatalf("Repair() error = %v, want post-publication stale view", err)
	}

	state, found, err := store.ActiveState(context.Background(), scope)
	// SQL precisely invalidates the published owner without manufacturing a new
	// marker/epoch. The original positive publication must become unusable.
	if err != nil || !found || state.BuildID != published.BuildID || state.Status != semanticstore.StatusDegraded || state.PublicationEpoch != 1 || state.ChunkCount != 0 || state.RecordCount != 0 {
		t.Fatalf("ActiveState() = (%#v, %t, %v), want exact W7 publication invalidated", state, found, err)
	}
}

//nolint:cyclop,lll,wsl_v5 // The winner identity and reconciled status jointly guard against stale failure overwrite.
func TestPostPublishRacePreservesConcurrentNewerWinnerAndReadyStatus(t *testing.T) {
	scope := testScope("newer-publication-winner")
	record := testRecord(scope, "item-a", "revision-a", "text", memory.SensitivityStandard, memory.RetentionClassStandard)
	canonical := newFakeCanonical(scope, 7, record)
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "sqlite-generation-7", 7)}
	store := openSemanticStore(t, canonical)
	older := newTestManager(t, canonical, manifests, &fakeProvider{vector: []float32{1, 0}}, store, testModelSpace("model-a"))
	newer := newTestManager(t, canonical, manifests, &fakeProvider{vector: []float32{0, 1}}, store, testModelSpace("model-a"))

	var winner semanticstore.State
	older.afterPublish = func() {
		canonical.replace(8, record)
		manifests.set(activeManifest(scope, "sqlite-generation-8", 8))
		if err := newer.Repair(context.Background(), scope); err != nil {
			t.Fatalf("newer Repair() error = %v", err)
		}
		var found bool
		var err error
		winner, found, err = store.ActiveState(context.Background(), scope)
		if err != nil || !found || winner.Status != semanticstore.StatusReady {
			t.Fatalf("newer ActiveState() = (%#v, %t, %v), want ready winner", winner, found, err)
		}
	}

	if err := older.Repair(context.Background(), scope); !errors.Is(err, ErrStaleView) {
		t.Fatalf("older Repair() error = %v, want stale view", err)
	}
	active, found, err := store.ActiveState(context.Background(), scope)
	if err != nil || !found || active.BuildID != winner.BuildID || active.Status != semanticstore.StatusReady ||
		active.LexicalGenerationID != "sqlite-generation-8" {
		t.Fatalf("ActiveState() = (%#v, %t, %v), want preserved W8 winner %#v", active, found, err, winner)
	}

	status, err := older.Status(context.Background(), scope)
	if err != nil || status.State != StateReady || status.LexicalWatermark != 8 {
		t.Fatalf("Status() = (%#v, %v), want reconciled W8 ready", status, err)
	}
}

func waitClosed(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()

	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal(message)
	}
}

func waitActiveState(
	t *testing.T,
	store *neutralSemanticStore,
	scope memory.Scope,
	ready func(semanticstore.State, bool) bool,
) semanticstore.State {
	t.Helper()

	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()

	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	for {
		state, found, err := store.ActiveState(context.Background(), scope)
		if err != nil {
			t.Fatalf("ActiveState() error = %v", err)
		}

		if ready(state, found) {
			return state
		}

		select {
		case <-deadline.C:
			t.Fatalf("ActiveState() = (%#v, %t), condition not reached", state, found)
		case <-ticker.C:
		}
	}
}

type generationBlockingProvider struct {
	mu            sync.Mutex
	calls         int
	firstEntered  chan struct{}
	releaseFirst  chan struct{}
	secondEntered chan struct{}
}

func newGenerationBlockingProvider() *generationBlockingProvider {
	return &generationBlockingProvider{
		firstEntered: make(chan struct{}), releaseFirst: make(chan struct{}), secondEntered: make(chan struct{}),
	}
}

func (provider *generationBlockingProvider) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	provider.mu.Lock()
	provider.calls++

	call := provider.calls
	if call == 1 {
		close(provider.firstEntered)
	}

	if call == 2 {
		close(provider.secondEntered)
	}
	provider.mu.Unlock()

	if call == 1 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-provider.releaseFirst:
		}
	}

	vectors := make([][]float32, len(inputs))
	for index := range vectors {
		vectors[index] = []float32{1, 0}
	}

	return vectors, nil
}

func (provider *generationBlockingProvider) Calls() int {
	provider.mu.Lock()
	defer provider.mu.Unlock()

	return provider.calls
}
