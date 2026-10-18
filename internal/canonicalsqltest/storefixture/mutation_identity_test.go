package storefixture_test

import (
	"bytes"
	"context"
	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"strings"
	"testing"
)

func TestPublicMutationLongIdentityAndHashCollision(t *testing.T) {
	forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
		mutation := nativeMutation(t)
		mutation.Scope.Key = "test:" + strings.Repeat("k", 65530)
		mutation.Sources[0].Scope = mutation.Scope
		mutation.Items[0].Scope = mutation.Scope
		mutation.Operation.OperationID = strings.Repeat("i", 65535)

		for _, entry := range []struct{ id, data string }{{"10187", "first collision"}, {"159793", "second collision"}} {
			data := []byte(entry.data)
			mutation.Payloads = append(mutation.Payloads, memory.CanonicalPayload{Ref: privacyRef(entry.id, data), Data: data})
		}

		release := probe.failInserts(t, "operation")
		if _, err := store.ApplyCanonicalMutation(t.Context(), mutation); err == nil {
			t.Fatal("long-key rollback fault ignored")
		}

		release()
		assertMutationState(t, store, mutation.Scope, 0)

		if _, err := store.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
			t.Fatal(err)
		}

		if _, err := store.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
			t.Fatal(err)
		}

		for _, payload := range mutation.Payloads[len(mutation.Payloads)-2:] {
			data, err := store.LoadPayload(t.Context(), payload.Ref)
			if err != nil || !bytes.Equal(data, payload.Data) {
				t.Fatalf("collision aliased payload: %v", err)
			}
		}

		mutation.Operation.OperationID = "stale"
		_, err := store.ApplyCanonicalMutation(t.Context(), mutation)
		assertMutationCode(t, err, memory.CodeConflict)
		assertMutationState(t, store, mutation.Scope, 1)
	})
}

func TestPublicMutationLargeChangePage(t *testing.T) {
	forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
		mutation := nativeMutation(t)
		if err := probe.connection.Update(t.Context(), func(tx *canonicalsql.Tx) error {
			for seq := uint64(1); seq <= 257; seq++ {
				key := canonicalsql.Key{Family: canonicalsql.ScopeChange, Scope: mutation.Scope,
					Parts: [][]byte{canonicalsql.Uint64(seq)}}

				data, err := canonicalsql.Encode(canonicalsql.ScopeChange, memory.ScopeChange{Sequence: seq,
					ScopeVersion: seq, OperationID: "change", OccurredAt: mutation.Operation.CommittedAt})
				if err != nil {
					return err
				}

				if err := tx.Put(context.Background(), key, data); err != nil {
					return err
				}
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}

		changes, err := store.ScanScopeChanges(t.Context(), mutation.Scope, 0, 512)
		if err != nil || len(changes) != 257 || changes[256].Sequence != 257 {
			t.Fatalf("large page count=%d error=%v", len(changes), err)
		}
	})
}
