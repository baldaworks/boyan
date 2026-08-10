package boyantest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// CanonicalStoreFactory creates an empty canonical store for one contract
// subtest. Implementations should register cleanup on t before returning it.
type CanonicalStoreFactory func(t *testing.T) memory.CanonicalStore

// RunCanonicalStoreContract applies the storage-neutral v2 canonical-store
// contract. Backend-specific tests retain ownership of reopen, fault, and
// physical-storage assertions.
func RunCanonicalStoreContract(t *testing.T, factory CanonicalStoreFactory) {
	t.Helper()
	if factory == nil {
		t.Fatal("boyantest: CanonicalStore factory is required")
	}
	t.Run("atomic_mutation_replay_change_log_and_collision", func(t *testing.T) {
		runCanonicalMutationContract(t, factory(t))
	})
	t.Run("stale_cas_and_exact_scope_isolation", func(t *testing.T) {
		runCanonicalScopeContract(t, factory(t))
	})
	t.Run("invalid_mutation_is_atomic", func(t *testing.T) {
		runCanonicalInvalidMutationContract(t, factory(t))
	})
	t.Run("cyclic_provenance_is_atomic", func(t *testing.T) {
		runCanonicalCycleContract(t, factory(t))
	})
	t.Run("independent_scopes_commit_concurrently", func(t *testing.T) {
		runCanonicalConcurrentScopesContract(t, factory(t))
	})
	t.Run("delivery_lease_settlement_and_recovery", func(t *testing.T) {
		runCanonicalDeliveryContract(t, factory(t))
	})
}

func runCanonicalMutationContract(t *testing.T, store memory.CanonicalStore) {
	t.Helper()
	scope := canonicalScope("contract:canonical-mutation")
	mutation := canonicalMutation(scope, 0, "operation-1", "revision-1", "item-1")
	first, err := store.ApplyCanonicalMutation(context.Background(), mutation)
	if err != nil {
		t.Fatalf("ApplyCanonicalMutation() error = %v", err)
	}
	if first.ScopeVersion != 1 || first.ChangeSeq != 1 || len(first.RevisionIDs) != 1 {
		t.Fatalf("first outcome = %#v", first)
	}
	replayed, err := store.ApplyCanonicalMutation(context.Background(), mutation)
	if err != nil || replayed.ScopeVersion != first.ScopeVersion || replayed.ChangeSeq != first.ChangeSeq || len(replayed.RevisionIDs) != len(first.RevisionIDs) || replayed.RevisionIDs[0] != first.RevisionIDs[0] {
		t.Fatalf("replayed outcome = %#v, error = %v; want %#v", replayed, err, first)
	}
	state, err := store.LoadScopeState(context.Background(), scope)
	if err != nil || state.Version != first.ScopeVersion || state.ChangeSeq != first.ChangeSeq {
		t.Fatalf("LoadScopeState() = %#v, error = %v", state, err)
	}
	changes, err := store.ScanScopeChanges(context.Background(), scope, 0, 2)
	if err != nil || len(changes) != 1 || changes[0].Sequence != 1 || changes[0].OperationID != mutation.Operation.OperationID {
		t.Fatalf("ScanScopeChanges() = %#v, error = %v", changes, err)
	}
	if changes, err := store.ScanScopeChanges(context.Background(), scope, 1, 2); err != nil || len(changes) != 0 {
		t.Fatalf("ScanScopeChanges(after latest) = %#v, error = %v", changes, err)
	}
	collision := mutation
	collision.Operation.Fingerprint = "different-fingerprint"
	if _, err := store.ApplyCanonicalMutation(context.Background(), collision); err == nil {
		t.Fatal("operation fingerprint collision succeeded")
	} else {
		requireCanonicalCode(t, err, memory.CodeConflict)
	}
}

func runCanonicalScopeContract(t *testing.T, store memory.CanonicalStore) {
	t.Helper()
	scopeA := canonicalScope("contract:canonical-scope-a")
	scopeB := canonicalScope("contract:canonical-scope-b")
	if _, err := store.ApplyCanonicalMutation(context.Background(), canonicalMutation(scopeA, 0, "operation-1", "revision-1", "item-1")); err != nil {
		t.Fatalf("first ApplyCanonicalMutation() error = %v", err)
	}
	if _, err := store.ApplyCanonicalMutation(context.Background(), canonicalMutation(scopeA, 0, "operation-2", "revision-2", "item-2")); err == nil {
		t.Fatal("stale CAS mutation succeeded")
	} else {
		requireCanonicalCode(t, err, memory.CodeConflict)
	}
	if _, err := store.ApplyCanonicalMutation(context.Background(), canonicalMutation(scopeB, 0, "operation-1", "revision-1", "item-1")); err != nil {
		t.Fatalf("same IDs in an independent scope error = %v", err)
	}
	stateA, err := store.LoadScopeState(context.Background(), scopeA)
	if err != nil || stateA.Version != 1 {
		t.Fatalf("scope A state = %#v, error = %v", stateA, err)
	}
	stateB, err := store.LoadScopeState(context.Background(), scopeB)
	if err != nil || stateB.Version != 1 {
		t.Fatalf("scope B state = %#v, error = %v", stateB, err)
	}
}

func runCanonicalInvalidMutationContract(t *testing.T, store memory.CanonicalStore) {
	t.Helper()
	scope := canonicalScope("contract:canonical-invalid")
	mutation := canonicalMutation(scope, 0, "operation-1", "revision-1", "item-1")
	mutation.Heads[0].RevisionID = "missing-revision"
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err == nil {
		t.Fatal("dangling head mutation succeeded")
	}
	state, err := store.LoadScopeState(context.Background(), scope)
	if err != nil || state.Version != 0 || state.ChangeSeq != 0 {
		t.Fatalf("state after rejected mutation = %#v, error = %v", state, err)
	}
}

func runCanonicalCycleContract(t *testing.T, store memory.CanonicalStore) {
	t.Helper()
	scope := canonicalScope("contract:canonical-cycle")
	mutation := canonicalMutation(scope, 0, "operation-1", "revision-1", "item-1")
	second := canonicalRevision("revision-2", "item-2", mutation.Operation.CommittedAt)
	mutation.Items = append(mutation.Items, memory.MemoryItem{ItemID: second.ItemID, Scope: scope, Kind: memory.MemoryKindEvent})
	mutation.Revisions[0].Parents = []string{second.RevisionID}
	second.Parents = []string{mutation.Revisions[0].RevisionID}
	mutation.Revisions = append(mutation.Revisions, second)
	mutation.Heads = append(mutation.Heads, memory.ItemHead{ItemID: second.ItemID, RevisionID: second.RevisionID})
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err == nil {
		t.Fatal("cyclic provenance mutation succeeded")
	}
	state, err := store.LoadScopeState(context.Background(), scope)
	if err != nil || state.Version != 0 || state.ChangeSeq != 0 {
		t.Fatalf("state after rejected cycle = %#v, error = %v", state, err)
	}
}

func runCanonicalConcurrentScopesContract(t *testing.T, store memory.CanonicalStore) {
	t.Helper()
	keys := []string{"contract:canonical-concurrent-a", "contract:canonical-concurrent-b"}
	errs := make(chan error, len(keys))
	var group sync.WaitGroup
	for _, key := range keys {
		group.Add(1)
		go func(key string) {
			defer group.Done()
			scope := canonicalScope(key)
			_, err := store.ApplyCanonicalMutation(context.Background(), canonicalMutation(scope, 0, "operation-1", "revision-1", "item-1"))
			errs <- err
		}(key)
	}
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("independent scope mutation error = %v", err)
		}
	}
	for _, key := range keys {
		state, err := store.LoadScopeState(context.Background(), canonicalScope(key))
		if err != nil || state.Version != 1 || state.ChangeSeq != 1 {
			t.Fatalf("scope %q state = %#v, error = %v", key, state, err)
		}
	}
}

func runCanonicalDeliveryContract(t *testing.T, store memory.CanonicalStore) {
	t.Helper()
	scope := canonicalScope("contract:canonical-delivery")
	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	mutation := canonicalMutation(scope, 0, "operation-1", "revision-1", "item-1")
	mutation.Delivery = []memory.DeliveryOutboxRecord{{
		DeliveryID: "delivery-1", OperationID: mutation.Operation.OperationID, CreatedAt: now,
		PayloadRef: memory.PayloadRef{ID: "payload-1", Digest: "digest-1", ByteSize: 1},
	}}
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatalf("ApplyCanonicalMutation() error = %v", err)
	}
	claim := memory.DeliveryClaimRequest{Scope: scope, LeaseOwner: "worker-1", Now: now, LeaseUntil: now.Add(time.Minute), Limit: 1}
	claimed, err := store.ClaimDeliveryOutbox(context.Background(), claim)
	if err != nil || len(claimed) != 1 || claimed[0].Claim.Attempts != 1 {
		t.Fatalf("ClaimDeliveryOutbox() = %#v, error = %v", claimed, err)
	}
	if err := store.SettleDeliveryOutbox(context.Background(), memory.DeliverySettlementRequest{
		Scope: scope, DeliveryID: "delivery-1", LeaseOwner: "worker-2", Status: memory.DeliveryStatusDelivered, CompletedAt: now,
	}); err == nil {
		t.Fatal("foreign worker settled an active lease")
	}
	if err := store.SettleDeliveryOutbox(context.Background(), memory.DeliverySettlementRequest{
		Scope: scope, DeliveryID: "delivery-1", LeaseOwner: "worker-1", Status: memory.DeliveryStatusDelivered, CompletedAt: now,
	}); err != nil {
		t.Fatalf("SettleDeliveryOutbox() error = %v", err)
	}
	if claimed, err := store.ClaimDeliveryOutbox(context.Background(), claim); err != nil || len(claimed) != 0 {
		t.Fatalf("settled delivery claim = %#v, error = %v", claimed, err)
	}
	second := canonicalMutation(scope, 1, "operation-2", "revision-2", "item-2")
	second.Delivery = []memory.DeliveryOutboxRecord{{
		DeliveryID: "delivery-2", OperationID: second.Operation.OperationID, CreatedAt: now,
		PayloadRef: memory.PayloadRef{ID: "payload-2", Digest: "digest-2", ByteSize: 1},
	}}
	if _, err := store.ApplyCanonicalMutation(context.Background(), second); err != nil {
		t.Fatalf("second ApplyCanonicalMutation() error = %v", err)
	}
	claimed, err = store.ClaimDeliveryOutbox(context.Background(), claim)
	if err != nil || len(claimed) != 1 || claimed[0].Record.DeliveryID != "delivery-2" || claimed[0].Claim.Attempts != 1 {
		t.Fatalf("second ClaimDeliveryOutbox() = %#v, error = %v", claimed, err)
	}
	claim.Now = claim.LeaseUntil.Add(time.Second)
	claim.LeaseUntil = claim.Now.Add(time.Minute)
	claimed, err = store.ClaimDeliveryOutbox(context.Background(), claim)
	if err != nil || len(claimed) != 1 || claimed[0].Record.DeliveryID != "delivery-2" || claimed[0].Claim.Attempts != 2 {
		t.Fatalf("recovered ClaimDeliveryOutbox() = %#v, error = %v", claimed, err)
	}
}

func canonicalScope(key string) memory.Scope {
	return memory.Scope{Key: key, Kind: memory.ScopeKindPersonal}
}

func canonicalMutation(scope memory.Scope, expectedVersion uint64, operationID, revisionID, itemID string) memory.CanonicalMutation {
	committedAt := time.Date(2026, time.August, 5, 12, 0, int(expectedVersion), 0, time.UTC)
	return memory.CanonicalMutation{
		SchemaVersion:        memory.CanonicalSchemaVersionV1,
		Scope:                scope,
		ExpectedScopeVersion: expectedVersion,
		Operation:            memory.OperationRecord{OperationID: operationID, Fingerprint: operationID + "-fingerprint", CommittedAt: committedAt},
		Items:                []memory.MemoryItem{{ItemID: itemID, Scope: scope, Kind: memory.MemoryKindEvent}},
		Revisions:            []memory.MemoryRevision{canonicalRevision(revisionID, itemID, committedAt)},
		Heads:                []memory.ItemHead{{ItemID: itemID, RevisionID: revisionID}},
	}
}

func canonicalRevision(revisionID, itemID string, observedAt time.Time) memory.MemoryRevision {
	return memory.MemoryRevision{
		SchemaVersion: memory.MemorySchemaVersion, RevisionID: revisionID, ItemID: itemID, Revision: 1,
		Temporal:    memory.Temporal{ObservedAt: observedAt},
		Evidence:    []memory.EvidenceRef{{SourceID: "source-1", MessageID: "message-1", Role: memory.MessageRoleUser, StartByte: 1, EndByte: 2, AssertionMode: memory.AssertionModeUser}},
		Sensitivity: memory.SensitivityStandard, Retention: memory.RetentionClassStandard,
		Payload: memory.PayloadRef{ID: "payload-1", Digest: "digest-1", ByteSize: 1},
	}
}

func requireCanonicalCode(t *testing.T, err error, want memory.ErrorCode) {
	t.Helper()
	var memoryErr *memory.Error
	if !errors.As(err, &memoryErr) || memoryErr.Code != want {
		t.Fatalf("error = %v, want sessionmemory code %q", err, want)
	}
}
