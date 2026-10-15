package semanticprojection //nolint:testpackage // Exercises private deterministic race seams.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	semanticstore "github.com/baldaworks/boyan/index/semantic"
	semanticcore "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

//nolint:lll,wsl_v5 // The complete eligibility fixture is intentionally explicit at the call site.
func TestRepairRequiresCertifiedPhysicalLexicalView(t *testing.T) {
	scope := testScope("physical-certification")
	record := testRecord(scope, "item-a", "revision-a", "private text", memory.SensitivityStandard, memory.RetentionClassStandard)
	canonical := newFakeCanonical(scope, 8, record)
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "bleve-generation-8", 8)}
	manifests.setPhysical(activeManifest(scope, "bleve-generation-7", 7))

	provider := &fakeProvider{vector: []float32{1, 0}}
	manager := newTestManager(t, canonical, manifests, provider, openSemanticStore(t), testModelSpace("model-a"))

	if err := manager.Repair(context.Background(), scope); !errors.Is(err, ErrStaleView) {
		t.Fatalf("Repair() error = %v, want stale physical view", err)
	}
	if inputs := provider.Inputs(); len(inputs) != 0 {
		t.Fatalf("provider inputs = %#v, want physical certification before inference", inputs)
	}
}

//nolint:lll,wsl_v5 // Race setup and expected physical tuples remain adjacent for auditability.
func TestRepairRechecksPhysicalViewImmediatelyBeforeAndAfterPublish(t *testing.T) {
	for _, test := range []struct {
		name string
		hook func(*Manager, func())
	}{
		{name: "before", hook: func(manager *Manager, change func()) { manager.beforePublish = change }},
		{name: "after", hook: func(manager *Manager, change func()) { manager.afterPublish = change }},
	} {
		t.Run(test.name, func(t *testing.T) {
			scope := testScope("physical-race-" + test.name)
			record := testRecord(scope, "item-a", "revision-a", "private text", memory.SensitivityStandard, memory.RetentionClassStandard)
			canonical := newFakeCanonical(scope, 7, record)
			manifests := &fakeManifestSource{manifest: activeManifest(scope, "bleve-generation-7", 7)}
			provider := &fakeProvider{vector: []float32{1, 0}}
			store := openSemanticStore(t)
			manager := newTestManager(t, canonical, manifests, provider, store, testModelSpace("model-a"))
			test.hook(manager, func() { manifests.setPhysical(activeManifest(scope, "bleve-generation-8", 8)) })

			if err := manager.Repair(context.Background(), scope); !errors.Is(err, ErrStaleView) {
				t.Fatalf("Repair() error = %v, want stale physical view", err)
			}

			state, found, err := store.ActiveState(context.Background(), scope)
			if err != nil {
				t.Fatalf("ActiveState() error = %v", err)
			}
			if found && state.Status == semanticstore.StatusReady {
				t.Fatalf("ActiveState() = %#v, raced generation remained ready", state)
			}
		})
	}
}

//nolint:lll,wsl_v5 // Exact temporal boundary fixtures remain explicit.
func TestRepairFiltersTemporalBoundariesBeforeProviderAtOneBuildInstant(t *testing.T) {
	scope := testScope("temporal")
	at := time.Unix(100, 0).UTC()
	expired := testRecord(scope, "item-expired", "revision-expired", "expired", memory.SensitivityStandard, memory.RetentionClassStandard)
	expired.Temporal.ExpiresAt = timePointer(at)
	future := testRecord(scope, "item-future", "revision-future", "future", memory.SensitivityStandard, memory.RetentionClassStandard)
	future.Temporal.ValidFrom = timePointer(at.Add(time.Nanosecond))
	eligible := testRecord(scope, "item-eligible", "revision-eligible", "eligible", memory.SensitivityStandard, memory.RetentionClassStandard)
	eligible.Temporal.ValidFrom = timePointer(at)
	eligible.Temporal.ValidUntil = timePointer(at.Add(time.Nanosecond))
	canonical := newFakeCanonical(scope, 7, expired, future, eligible)
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "bleve-generation-7", 7)}
	provider := &fakeProvider{vector: []float32{1, 0}}
	store := openSemanticStore(t)
	manager := newTestManager(t, canonical, manifests, provider, store, testModelSpace("model-a"))
	manager.now = func() time.Time { return at }

	if err := manager.Repair(context.Background(), scope); err != nil {
		t.Fatalf("Repair() error = %v", err)
	}
	if inputs := provider.Inputs(); len(inputs) != 1 || inputs[0] != "passage: eligible" {
		t.Fatalf("provider inputs = %#v, want only boundary-eligible record", inputs)
	}

	state, found, err := store.ActiveState(context.Background(), scope)
	if err != nil || !found || state.Omitted.Expired != 1 || state.Omitted.Ineligible != 1 {
		t.Fatalf("ActiveState() = (%#v, %t, %v), want expired and future-ineligible omissions", state, found, err)
	}
}

//nolint:wsl_v5 // Page overflow and provider-inertness assertions are one contract.
func TestRepairRejectsInvalidAndUnboundedActivePagesBeforeInference(t *testing.T) {
	scope := testScope("bad-page")
	canonical := newFakeCanonical(scope, 7)
	canonical.scanOverride = func(memory.ActiveMemoryScanRequest) []memory.ActiveCanonicalMemory {
		return make([]memory.ActiveCanonicalMemory, canonicalPageSize+1)
	}
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "bleve-generation-7", 7)}
	provider := &fakeProvider{vector: []float32{1, 0}}
	manager := newTestManager(t, canonical, manifests, provider, openSemanticStore(t), testModelSpace("model-a"))

	if err := manager.Repair(context.Background(), scope); !errors.Is(err, semanticstore.ErrCapacity) {
		t.Fatalf("Repair() error = %v, want capacity", err)
	}
	if inputs := provider.Inputs(); len(inputs) != 0 {
		t.Fatalf("provider inputs = %#v, want page validation before inference", inputs)
	}
}

//nolint:lll,wsl_v5 // Full active identities make the cursor failure self-contained.
func TestRepairRejectsUnstableActivePageCursorBeforeInference(t *testing.T) {
	scope := testScope("bad-cursor")
	canonical := newFakeCanonical(scope, 7)
	canonical.scanOverride = func(memory.ActiveMemoryScanRequest) []memory.ActiveCanonicalMemory {
		return []memory.ActiveCanonicalMemory{
			{Item: memory.MemoryItem{ItemID: "item-b", Scope: scope, Kind: memory.MemoryKindState, MemoryKey: "key-b"}, RevisionID: "revision-b", Revision: 1},
			{Item: memory.MemoryItem{ItemID: "item-a", Scope: scope, Kind: memory.MemoryKindState, MemoryKey: "key-a"}, RevisionID: "revision-a", Revision: 1},
		}
	}
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "bleve-generation-7", 7)}
	provider := &fakeProvider{vector: []float32{1, 0}}
	manager := newTestManager(t, canonical, manifests, provider, openSemanticStore(t), testModelSpace("model-a"))

	if err := manager.Repair(context.Background(), scope); !errors.Is(err, ErrStaleView) {
		t.Fatalf("Repair() error = %v, want stale cursor", err)
	}
	if inputs := provider.Inputs(); len(inputs) != 0 {
		t.Fatalf("provider inputs = %#v, want cursor validation before inference", inputs)
	}
}

//nolint:lll,wsl_v5 // Exact and one-over scan fixtures intentionally show all eligibility fields.
func TestRepairBoundsTotalScannedRecordsIncludingIneligible(t *testing.T) {
	for _, count := range []int{semanticcore.ProjectionScanRecordLimit, semanticcore.ProjectionScanRecordLimit + 1} {
		t.Run(canonicalTestID("count", count), func(t *testing.T) {
			scope := testScope(canonicalTestID("scan-bound", count))

			records := make([]memory.RecallRecord, count)
			for index := range records {
				records[index] = testRecord(scope, canonicalTestID("item", index), canonicalTestID("revision", index), "excluded", memory.SensitivitySensitive, memory.RetentionClassStandard)
			}
			canonical := newFakeCanonical(scope, 7, records...)
			manifests := &fakeManifestSource{manifest: activeManifest(scope, "bleve-generation-7", 7)}
			provider := &fakeProvider{vector: []float32{1, 0}}
			manager := newTestManager(t, canonical, manifests, provider, openSemanticStore(t), testModelSpace("model-a"))

			err := manager.Repair(context.Background(), scope)
			if count == semanticcore.ProjectionScanRecordLimit && err != nil {
				t.Fatalf("Repair(exact scan bound) error = %v", err)
			}
			if count > semanticcore.ProjectionScanRecordLimit && !errors.Is(err, semanticstore.ErrCapacity) {
				t.Fatalf("Repair(one-over scan bound) error = %v, want capacity", err)
			}
			if inputs := provider.Inputs(); len(inputs) != 0 {
				t.Fatalf("provider inputs = %#v, want all sensitive rows excluded", inputs)
			}
		})
	}
}

//nolint:lll,wsl_v5 // High-dimension capacity setup is intentionally explicit.
func TestRepairPreflightsHighDimensionEncodedBudgetBeforeInference(t *testing.T) {
	scope := testScope("byte-budget")

	records := make([]memory.RecallRecord, 3200)
	for index := range records {
		records[index] = testRecord(scope, canonicalTestID("item", index), canonicalTestID("revision", index), "x", memory.SensitivityStandard, memory.RetentionClassStandard)
	}

	canonical := newFakeCanonical(scope, 7, records...)
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "bleve-generation-7", 7)}
	provider := &fakeProvider{vector: make([]float32, semanticcore.MaxEmbeddingDimensions)}
	space := testModelSpace("model-a")
	space.Dimensions = semanticcore.MaxEmbeddingDimensions
	manager := newTestManager(t, canonical, manifests, provider, openSemanticStore(t), space)

	if err := manager.Repair(context.Background(), scope); !errors.Is(err, semanticstore.ErrCapacity) {
		t.Fatalf("Repair() error = %v, want encoded-byte capacity", err)
	}
	if inputs := provider.Inputs(); len(inputs) != 0 {
		t.Fatalf("provider received %d inputs before encoded budget rejection", len(inputs))
	}
}

//nolint:lll // Both manager configurations remain visible at the status boundary.
func TestStatusRevalidatesModelAndCertifiedPhysicalView(t *testing.T) {
	scope := testScope("status-compatibility")
	record := testRecord(scope, "item-a", "revision-a", "text", memory.SensitivityStandard, memory.RetentionClassStandard)
	canonical := newFakeCanonical(scope, 7, record)
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "bleve-generation-7", 7)}
	store := openSemanticStore(t)

	first := newTestManager(t, canonical, manifests, &fakeProvider{vector: []float32{1, 0}}, store, testModelSpace("model-a"))
	if err := first.Repair(context.Background(), scope); err != nil {
		t.Fatalf("Repair() error = %v", err)
	}

	changed := newTestManager(t, canonical, manifests, &fakeProvider{vector: []float32{1, 0}}, store, testModelSpace("model-b"))

	status, err := changed.Status(context.Background(), scope)
	if err != nil || status.State != StateDegraded || status.SafeReason != semanticcore.ReasonUnavailable {
		t.Fatalf("changed-space Status() = (%#v, %v), want degraded", status, err)
	}

	manifests.setPhysical(activeManifest(scope, "bleve-generation-8", 8))

	status, err = first.Status(context.Background(), scope)
	if err != nil || status.State != StateDegraded {
		t.Fatalf("changed-physical Status() = (%#v, %v), want degraded", status, err)
	}
}

//nolint:lll,wsl_v5 // Queue lifecycle setup remains explicit.
func TestScheduleImmediatelyTransitionsOldReadyStatusToRepairing(t *testing.T) {
	scope := testScope("queued-refresh")
	record := testRecord(scope, "item-a", "revision-a", "text", memory.SensitivityStandard, memory.RetentionClassStandard)
	canonical := newFakeCanonical(scope, 7, record)
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "bleve-generation-7", 7)}

	manager := newTestManager(t, canonical, manifests, &fakeProvider{vector: []float32{1, 0}}, openSemanticStore(t), testModelSpace("model-a"))
	if err := manager.Repair(context.Background(), scope); err != nil {
		t.Fatalf("Repair() error = %v", err)
	}

	release := make(chan struct{})

	manager.beforeBuild = func(ctx context.Context, _ memory.Scope) {
		select {
		case <-release:
		case <-ctx.Done():
		}
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := manager.Schedule(scope); err != nil {
		t.Fatalf("Schedule() error = %v", err)
	}

	status, err := manager.Status(context.Background(), scope)
	if err != nil || status.State != StateRepairing {
		t.Fatalf("queued Status() = (%#v, %v), want repairing", status, err)
	}

	close(release)
}

//nolint:cyclop,lll,wsl_v5 // Source and scope forget transitions share one persistent-store contract.
func TestRepairForgettingSourceAndScopePublishesNoForgottenChunks(t *testing.T) {
	scope := testScope("forget")
	forgotten := testRecord(scope, "item-a", "revision-a", "forgotten", memory.SensitivityStandard, memory.RetentionClassStandard)
	forgotten.SourceIDs = []string{"source-a"}
	retained := testRecord(scope, "item-b", "revision-b", "retained", memory.SensitivityStandard, memory.RetentionClassStandard)
	retained.SourceIDs = []string{"source-b"}
	canonical := newFakeCanonical(scope, 7, forgotten, retained)
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "bleve-generation-7", 7)}
	store := openSemanticStore(t)

	manager := newTestManager(t, canonical, manifests, &fakeProvider{vector: []float32{1, 0}}, store, testModelSpace("model-a"))
	if err := manager.Repair(context.Background(), scope); err != nil {
		t.Fatalf("initial Repair() error = %v", err)
	}

	for _, transition := range []struct {
		name     string
		sequence uint64
		records  []memory.RecallRecord
		want     string
	}{
		{name: "forget source", sequence: 8, records: []memory.RecallRecord{retained}, want: "revision-b"},
		{name: "forget scope", sequence: 9, records: nil},
	} {
		t.Run(transition.name, func(t *testing.T) {
			canonical.replace(transition.sequence, transition.records...)
			manifests.set(activeManifest(scope, canonicalTestID("bleve-generation", int(transition.sequence)), transition.sequence))
			if err := manager.Repair(context.Background(), scope); err != nil {
				t.Fatalf("Repair() error = %v", err)
			}

			state, found, err := store.ActiveState(context.Background(), scope)
			if err != nil || !found || state.Status != semanticstore.StatusReady {
				t.Fatalf("ActiveState() = (%#v, %t, %v), want ready replacement", state, found, err)
			}
			snapshot, compatible, err := store.Snapshot(context.Background(), state.View())
			if err != nil || !compatible {
				t.Fatalf("Snapshot() = (_, %t, %v), want compatible", compatible, err)
			}

			chunks := snapshot.Chunks()
			if transition.want == "" && len(chunks) != 0 {
				t.Fatalf("scope-forget chunks = %#v, want empty", chunks)
			}

			if transition.want != "" && (len(chunks) != 1 || chunks[0].RevisionID != transition.want) {
				t.Fatalf("source-forget chunks = %#v, want only %q", chunks, transition.want)
			}
		})
	}
}

//nolint:cyclop,lll,wsl_v5 // Cancellation and deadline must traverse the same lifecycle assertions.
func TestCancellationDeadlineAndCloseDoNotLeaveRepairingStatus(t *testing.T) {
	for _, test := range []struct {
		name       string
		deadline   bool
		wantReason semanticcore.Reason
	}{
		{name: "caller cancellation", wantReason: semanticcore.ReasonUnavailable},
		{name: "build deadline", deadline: true, wantReason: semanticcore.ReasonDeadline},
	} {
		t.Run(test.name, func(t *testing.T) {
			scope := testScope("lifecycle-" + test.name)
			record := testRecord(scope, "item-a", "revision-a", "text", memory.SensitivityStandard, memory.RetentionClassStandard)
			canonical := newFakeCanonical(scope, 7, record)
			provider := &cancelProvider{entered: make(chan struct{})}

			manager, err := New(Dependencies{
				Canonical: canonical, Records: canonical,
				Manifests: &fakeManifestSource{manifest: activeManifest(scope, "bleve-generation-7", 7)},
				Physical:  &fakeManifestSource{manifest: activeManifest(scope, "bleve-generation-7", 7)},
				Provider:  provider, Store: openSemanticStore(t), ModelSpace: testModelSpace("model-a"),
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			if test.deadline {
				manager.buildTimeout = 10 * time.Millisecond
			} else {
				go func() { <-provider.entered; cancel() }()
			}

			err = manager.Repair(ctx, scope)
			if test.deadline && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Repair() error = %v, want deadline", err)
			}
			if !test.deadline && !errors.Is(err, context.Canceled) {
				t.Fatalf("Repair() error = %v, want cancellation", err)
			}

			status, statusErr := manager.Status(context.Background(), scope)
			if statusErr != nil || status.State != StateDegraded || status.SafeReason != test.wantReason {
				t.Fatalf("Status() = (%#v, %v), want degraded %q", status, statusErr, test.wantReason)
			}
			if closeErr := manager.Close(context.Background()); closeErr != nil {
				t.Fatalf("Close() error = %v", closeErr)
			}
			status, statusErr = manager.Status(context.Background(), scope)
			if statusErr != nil || status.State != StateStopped {
				t.Fatalf("closed Status() = (%#v, %v), want stopped", status, statusErr)
			}
		})
	}
}

//nolint:wsl_v5 // Flight admission and both completions remain visibly ordered.
func TestConcurrentRepairsShareOneSuccessfulFlightCompletion(t *testing.T) {
	scope := testScope("single-flight")
	record := testRecord(scope, "item-a", "revision-a", "text", memory.SensitivityStandard, memory.RetentionClassStandard)
	canonical := newFakeCanonical(scope, 7, record)
	provider := &singleFlightProvider{entered: make(chan struct{}), release: make(chan struct{})}
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "bleve-generation-7", 7)}

	manager, err := New(Dependencies{
		Canonical: canonical, Records: canonical, Manifests: manifests, Physical: manifests,
		Provider: provider, Store: openSemanticStore(t), ModelSpace: testModelSpace("model-a"),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	joined := make(chan struct{})
	manager.joinedFlight = func() { close(joined) }

	results := make(chan error, 2)
	go func() { results <- manager.Repair(context.Background(), scope) }()

	<-provider.entered

	go func() { results <- manager.Repair(context.Background(), scope) }()

	<-joined
	close(provider.release)

	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("coalesced Repair() error = %v", err)
		}
	}

	if provider.Calls() != 1 {
		t.Fatalf("provider calls = %d, want one completed flight", provider.Calls())
	}
}

func timePointer(value time.Time) *time.Time { return &value }

func canonicalTestID(prefix string, index int) string {
	return prefix + "-" + time.Unix(int64(index), 0).UTC().Format("150405.000000000")
}

type singleFlightProvider struct {
	mu      sync.Mutex
	entered chan struct{}
	release chan struct{}
	calls   int
}

func (provider *singleFlightProvider) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	provider.mu.Lock()

	provider.calls++
	if provider.calls == 1 {
		close(provider.entered)
	}
	provider.mu.Unlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-provider.release:
	}

	vectors := make([][]float32, len(inputs))
	for index := range vectors {
		vectors[index] = []float32{1, 0}
	}

	return vectors, nil
}

func (provider *singleFlightProvider) Calls() int {
	provider.mu.Lock()
	defer provider.mu.Unlock()

	return provider.calls
}
