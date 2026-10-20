//go:build integration

//nolint:cyclop,funlen // Real lease lifecycle retains restart, ownership and terminal transitions.
package storefixture_test

import (
	"context"
	"math"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

type deliveryStore interface {
	ClaimDeliveryOutbox(ctx context.Context, request memory.DeliveryClaimRequest) ([]memory.ClaimedDelivery, error)
	SettleDeliveryOutbox(ctx context.Context, request memory.DeliverySettlementRequest) error
}

func deliveryFixture(
	t *testing.T,
	run func(*testing.T, *storefixture.Fixture, mutationStore, deliveryStore, mutationProbe),
) {
	t.Helper()
	forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, s mutationStore, p mutationProbe) {
		d, ok := f.Store.(deliveryStore)
		if !ok {
			t.Fatal("SQL owner does not implement delivery lease transitions")
		}

		run(t, f, s, d, p)
	})
}

func seedDeliveries(t *testing.T, s mutationStore, ids ...string) memory.DeliveryClaimRequest {
	t.Helper()

	m := nativeMutation(t)
	for _, id := range ids {
		m.Delivery = append(m.Delivery, memory.DeliveryOutboxRecord{
			DeliveryID:  id,
			OperationID: m.Operation.OperationID,
			CreatedAt:   m.Operation.CommittedAt,
			PayloadRef:  m.Revisions[0].Payload,
		})
	}

	if _, err := s.ApplyCanonicalMutation(t.Context(), m); err != nil {
		t.Fatal(err)
	}

	now := m.Operation.CommittedAt.Add(time.Minute)

	return memory.DeliveryClaimRequest{Scope: m.Scope, LeaseOwner: "worker", Now: now,
		LeaseUntil: now.Add(time.Minute), Limit: 1}
}

func TestPublicDeliveryLeaseRecoveryAndSettlement(t *testing.T) {
	deliveryFixture(t, func(t *testing.T, f *storefixture.Fixture, s mutationStore, d deliveryStore, p mutationProbe) {
		r := seedDeliveries(t, s, "b", "a", "c")

		first, err := d.ClaimDeliveryOutbox(t.Context(), r)
		if err != nil || len(first) != 1 || first[0].Record.DeliveryID != "a" || first[0].Claim.Attempts != 1 ||
			first[0].Claim.LeaseOwner != r.LeaseOwner || !first[0].Claim.LeaseUntil.Equal(r.LeaseUntil) {
			t.Fatalf("first=%+v err=%v", first, err)
		}

		settle := memory.DeliverySettlementRequest{Scope: r.Scope, DeliveryID: "a", LeaseOwner: "foreign",
			Status: memory.DeliveryStatusDelivered, CompletedAt: r.Now}
		assertMutationCode(t, d.SettleDeliveryOutbox(t.Context(), settle), memory.CodeConflict)
		settle.DeliveryID = "missing"
		assertMutationCode(t, d.SettleDeliveryOutbox(t.Context(), settle), memory.CodeNotFound)

		if err := f.Store.Close(); err != nil {
			t.Fatal(err)
		}

		reopened, err := f.Open(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = reopened.Close() })

		d = reopened.(deliveryStore)
		r.Now = r.LeaseUntil
		r.LeaseUntil = r.Now.Add(2 * time.Minute)
		r.LeaseOwner = "replacement"

		claimed, err := d.ClaimDeliveryOutbox(t.Context(), r)
		if err != nil || len(claimed) != 1 || claimed[0].Record.DeliveryID != "a" || claimed[0].Claim.Attempts != 2 ||
			!claimed[0].Claim.LeaseUntil.Equal(r.LeaseUntil) {
			t.Fatalf("reclaimed=%+v err=%v", claimed, err)
		}

		settle.DeliveryID, settle.LeaseOwner = "a", "worker"
		assertMutationCode(t, d.SettleDeliveryOutbox(t.Context(), settle), memory.CodeConflict)
		settle.LeaseOwner = r.LeaseOwner
		// Settlement trusts the leased status/owner, even after the supplied expiry.
		settle.CompletedAt = r.LeaseUntil.Add(time.Hour)
		if err := d.SettleDeliveryOutbox(t.Context(), settle); err != nil {
			t.Fatal(err)
		}

		assertMutationCode(t, d.SettleDeliveryOutbox(t.Context(), settle), memory.CodeConflict)

		claimed, err = d.ClaimDeliveryOutbox(t.Context(), r)
		if err != nil || len(claimed) != 1 || claimed[0].Record.DeliveryID != "b" {
			t.Fatalf("next=%+v err=%v", claimed, err)
		}

		settle.DeliveryID, settle.Status = "b", memory.DeliveryStatusTerminal
		if err := d.SettleDeliveryOutbox(t.Context(), settle); err != nil {
			t.Fatal(err)
		}

		r.Now = r.LeaseUntil
		r.LeaseUntil = r.Now.Add(time.Minute)
		r.Limit = 3

		claimed, err = d.ClaimDeliveryOutbox(t.Context(), r)
		if err != nil || len(claimed) != 1 || claimed[0].Record.DeliveryID != "c" {
			t.Fatalf("terminal exclusion=%+v err=%v", claimed, err)
		}

		assertMutationState(t, reopened.(mutationStore), r.Scope, 1)

		var claim memory.DeliveryClaim
		p.read(t, r.Scope, canonicalsql.DeliveryClaim, "a", &claim)

		if claim.Attempts != 2 || claim.LeaseUntil != nil || claim.LeaseOwner != "" {
			t.Fatalf("settled=%+v", claim)
		}
	})
}

func TestPublicDeliveryConcurrentClaimsAndAtomicFailure(t *testing.T) {
	deliveryFixture(t, func(t *testing.T, _ *storefixture.Fixture, s mutationStore, d deliveryStore, p mutationProbe) {
		r := seedDeliveries(t, s, "a", "b")
		before := p.snapshot(t)
		release := p.failInserts(t, "delivery_claim")
		_, err := d.ClaimDeliveryOutbox(t.Context(), r)

		release()

		if err == nil || !reflect.DeepEqual(before, p.snapshot(t)) {
			t.Fatalf("failed claim escaped: %v", err)
		}

		results := make(chan []memory.ClaimedDelivery, 2)
		failures := make(chan error, 2)

		var group sync.WaitGroup
		for _, owner := range []string{"one", "two"} {
			group.Go(func() {
				request := r
				request.LeaseOwner = owner

				got, err := d.ClaimDeliveryOutbox(t.Context(), request)
				results <- got

				failures <- err
			})
		}

		group.Wait()
		close(results)
		close(failures)

		for err := range failures {
			if err != nil {
				t.Fatal(err)
			}
		}

		seen := map[string]bool{}
		for got := range results {
			if len(got) != 1 || seen[got[0].Record.DeliveryID] {
				t.Fatalf("overlap=%+v", got)
			}

			seen[got[0].Record.DeliveryID] = true
		}

		if len(seen) != 2 {
			t.Fatalf("claimed=%v", seen)
		}

		got, err := d.ClaimDeliveryOutbox(t.Context(), r)
		if err != nil || len(got) != 0 {
			t.Fatalf("active claims=%+v err=%v", got, err)
		}

		foreign := r
		foreign.Scope.Kind = memory.ScopeKindGroup

		got, err = d.ClaimDeliveryOutbox(t.Context(), foreign)
		if err != nil || len(got) != 0 {
			t.Fatalf("foreign=%+v %v", got, err)
		}

		assertMutationState(t, s, r.Scope, 1)
	})
}

func TestPublicDeliveryNilExpiryAndAttemptWrap(t *testing.T) {
	deliveryFixture(t, func(t *testing.T, _ *storefixture.Fixture, s mutationStore, d deliveryStore, p mutationProbe) {
		r := seedDeliveries(t, s, "one")
		p.write(
			t,
			canonicalsql.Key{Family: canonicalsql.DeliveryClaim, Scope: r.Scope, Parts: [][]byte{[]byte("one")}},
			memory.DeliveryClaim{
				DeliveryID: "one",
				Status:     memory.DeliveryStatusLeased,
				LeaseOwner: "old",
				Attempts:   math.MaxUint32,
			},
		)

		got, err := d.ClaimDeliveryOutbox(t.Context(), r)
		if err != nil || len(got) != 1 || got[0].Claim.Attempts != 0 || got[0].Claim.LeaseOwner != "worker" {
			t.Fatalf("legacy reclaim=%+v %v", got, err)
		}
	})
}

func TestPublicDeliveryClaimStopsAtRequestedLimit(t *testing.T) {
	deliveryFixture(t, func(t *testing.T, _ *storefixture.Fixture, s mutationStore, d deliveryStore, p mutationProbe) {
		r := seedDeliveries(t, s, "a", "b")
		if err := p.exec(
			"UPDATE "+p.table("delivery")+" SET envelope=$1 WHERE id=$2",
			[]byte("invalid envelope"),
			[]byte("b"),
		); err != nil {
			t.Fatal(err)
		}

		got, err := d.ClaimDeliveryOutbox(t.Context(), r)
		if err != nil || len(got) != 1 || got[0].Record.DeliveryID != "a" {
			t.Fatalf("unvisited record affected bounded claim=%+v %v", got, err)
		}
	})
}
