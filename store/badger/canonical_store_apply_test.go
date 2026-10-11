package badger_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/memory"
	canonicalbadger "github.com/baldaworks/boyan/store/badger"
)

func TestCanonicalStoreAppliesCanonicalMutation(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "memory.badger")

	store := openCanonicalTestStore(t, directory)

	t.Cleanup(func() { _ = store.Close() })

	scope := boyan.Scope{Key: "canonical:badger", Kind: boyan.ScopeKindPersonal}
	mutation := boyan.CanonicalMutation{
		SchemaVersion: boyan.CanonicalSchemaVersionV1,
		Scope:         scope,
		Operation: boyan.OperationRecord{
			OperationID: "operation-1",
			Fingerprint: "derivation-current-sha256",
			CommittedAt: time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC),
		},
		Heads: []boyan.ItemHead{{ItemID: "item-1", RevisionID: "revision-1"}},
	}
	mutation.Items = []boyan.MemoryItem{canonicalItem(scope, "item-1")}
	mutation.Revisions = []boyan.MemoryRevision{canonicalRevision("revision-1", "item-1")}
	payloadData := []byte("canonical payload")
	payloadDigest := sha256.Sum256(payloadData)
	payloadRef := boyan.PayloadRef{
		ID:       "payload-1",
		Digest:   hex.EncodeToString(payloadDigest[:]),
		ByteSize: uint32(len(payloadData)),
	}
	mutation.Revisions[0].Payload = payloadRef
	mutation.Payloads = []boyan.CanonicalPayload{{Ref: payloadRef, Data: payloadData}}

	first, err := store.ApplyCanonicalMutation(context.Background(), mutation)
	if err != nil {
		t.Fatalf("ApplyCanonicalMutation() error = %v", err)
	}

	if first.ScopeVersion != 1 || first.ChangeSeq != 1 {
		t.Fatalf("outcome = %#v", first)
	}

	gotPayload, err := store.LoadPayload(context.Background(), mutation.Payloads[0].Ref)
	if err != nil || string(gotPayload) != string(payloadData) {
		t.Fatalf("LoadPayload() = %q, error = %v", gotPayload, err)
	}

	replayed, err := store.ApplyCanonicalMutation(context.Background(), mutation)
	if err != nil || replayed.ScopeVersion != first.ScopeVersion ||
		replayed.ChangeSeq != first.ChangeSeq {
		t.Fatalf("replay = %#v, error = %v, want %#v", replayed, err, first)
	}

	assertCanonicalMutationScopeState(t, store, scope, mutation)
}

func assertCanonicalMutationScopeState(
	t *testing.T,
	store *canonicalbadger.CanonicalStore,
	scope boyan.Scope,
	mutation boyan.CanonicalMutation,
) {
	t.Helper()

	state, err := store.LoadScopeState(context.Background(), scope)
	if err != nil || state.Version != 1 || state.ChangeSeq != 1 {
		t.Fatalf("LoadScopeState() = %#v, error = %v", state, err)
	}

	changes, err := store.ScanScopeChanges(context.Background(), scope, 0, 10)
	if err != nil || len(changes) != 1 ||
		changes[0].OperationID != mutation.Operation.OperationID {
		t.Fatalf("ScanScopeChanges() = %#v, error = %v", changes, err)
	}

	changes, err = store.ScanScopeChanges(context.Background(), scope, 1, 10)
	if err != nil || len(changes) != 0 {
		t.Fatalf("ScanScopeChanges(after latest) = %#v, error = %v", changes, err)
	}

	collision := mutation

	collision.Operation.Fingerprint = "different-fingerprint"
	if _, err := store.ApplyCanonicalMutation(context.Background(), collision); err == nil {
		t.Fatal("operation fingerprint collision succeeded")
	}
}
