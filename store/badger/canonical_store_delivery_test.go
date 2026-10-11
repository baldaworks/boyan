package badger_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/memory"
	canonicalbadger "github.com/baldaworks/boyan/store/badger"
)

func TestCanonicalStoreClaimsAndRecoversDeliveryLease(t *testing.T) {
	store := openCanonicalTestStore(t, filepath.Join(t.TempDir(), "memory.badger"))

	t.Cleanup(func() { _ = store.Close() })

	scope := boyan.Scope{Key: "canonical:delivery", Kind: boyan.ScopeKindPersonal}

	mutation := boyan.CanonicalMutation{
		SchemaVersion: boyan.CanonicalSchemaVersionV1,
		Scope:         scope,
		Operation: boyan.OperationRecord{
			OperationID: "operation-1",
			Fingerprint: "fingerprint-1",
			CommittedAt: time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC),
		},
		Delivery: []boyan.DeliveryOutboxRecord{{
			DeliveryID: "delivery-1", OperationID: "operation-1", CreatedAt: time.Date(
				2026,
				time.August,
				5,
				12,
				0,
				0,
				0,
				time.UTC,
			),
			PayloadRef: boyan.PayloadRef{ID: "payload-1", Digest: "digest-1", ByteSize: 1},
		}},
	}
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatalf("ApplyCanonicalMutation() error = %v", err)
	}

	now := time.Date(2026, time.August, 5, 12, 1, 0, 0, time.UTC)
	request := boyan.DeliveryClaimRequest{
		Scope:      scope,
		LeaseOwner: "worker-1",
		Now:        now,
		LeaseUntil: now.Add(time.Minute),
		Limit:      1,
	}

	first, err := store.ClaimDeliveryOutbox(context.Background(), request)
	if err != nil || len(first) != 1 || first[0].Claim.Attempts != 1 {
		t.Fatalf("first ClaimDeliveryOutbox() = %#v, error = %v", first, err)
	}

	if claimed, err := store.ClaimDeliveryOutbox(context.Background(), request); err != nil || len(claimed) != 0 {
		t.Fatalf("active lease claim = %#v, error = %v", claimed, err)
	}

	assertCanonicalDeliverySettlement(t, store, scope, request, now)

	assertCanonicalDeliveryLeaseRecovery(t, store, mutation, request, now)
}

func assertCanonicalDeliveryLeaseRecovery(
	t *testing.T,
	store *canonicalbadger.CanonicalStore,
	mutation boyan.CanonicalMutation,
	request boyan.DeliveryClaimRequest,
	now time.Time,
) {
	t.Helper()

	mutation.Operation.OperationID = "operation-2"
	mutation.Operation.Fingerprint = "fingerprint-2"
	mutation.Operation.CommittedAt = now
	mutation.ExpectedScopeVersion = 1
	mutation.Delivery[0].DeliveryID = "delivery-2"

	mutation.Delivery[0].OperationID = "operation-2"
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatalf("second ApplyCanonicalMutation() error = %v", err)
	}

	if claimed, err := store.ClaimDeliveryOutbox(context.Background(), request); err != nil || len(claimed) != 1 ||
		claimed[0].Record.DeliveryID != "delivery-2" {
		t.Fatalf("second delivery claim = %#v, error = %v", claimed, err)
	}

	request.Now = request.LeaseUntil.Add(time.Second)
	request.LeaseUntil = request.Now.Add(time.Minute)

	recovered, err := store.ClaimDeliveryOutbox(context.Background(), request)
	if err != nil || len(recovered) != 1 || recovered[0].Claim.Attempts != 2 {
		t.Fatalf("recovered ClaimDeliveryOutbox() = %#v, error = %v", recovered, err)
	}
}

func assertCanonicalDeliverySettlement(
	t *testing.T,
	store *canonicalbadger.CanonicalStore,
	scope boyan.Scope,
	request boyan.DeliveryClaimRequest,
	now time.Time,
) {
	t.Helper()

	if err := store.SettleDeliveryOutbox(context.Background(), boyan.DeliverySettlementRequest{
		Scope: scope, DeliveryID: "delivery-1", LeaseOwner: "worker-2",
		Status: boyan.DeliveryStatusDelivered, CompletedAt: now,
	}); err == nil {
		t.Fatal("foreign worker settled an active lease")
	}

	if err := store.SettleDeliveryOutbox(context.Background(), boyan.DeliverySettlementRequest{
		Scope: scope, DeliveryID: "delivery-1", LeaseOwner: "worker-1",
		Status: boyan.DeliveryStatusDelivered, CompletedAt: now,
	}); err != nil {
		t.Fatalf("SettleDeliveryOutbox() error = %v", err)
	}

	if claimed, err := store.ClaimDeliveryOutbox(context.Background(), request); err != nil || len(claimed) != 0 {
		t.Fatalf("settled delivery claim = %#v, error = %v", claimed, err)
	}
}
