package badger_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

func TestCanonicalStoreRejectsInvalidProvenanceAtomically(t *testing.T) {
	store := openCanonicalTestStore(t, filepath.Join(t.TempDir(), "memory.badger"))

	t.Cleanup(func() { _ = store.Close() })

	scope := boyan.Scope{Key: "canonical:provenance-invalid", Kind: boyan.ScopeKindPersonal}
	first := canonicalRevision("revision-1", "item-1")
	second := canonicalRevision("revision-2", "item-2")
	first.Parents = []string{second.RevisionID}
	second.Parents = []string{first.RevisionID}
	mutation := canonicalRevisionMutation(scope, 0, "operation-cycle", first)

	mutation.Revisions = append(mutation.Revisions, second)
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err == nil {
		t.Fatal("cyclic provenance mutation succeeded")
	}

	state, err := store.LoadScopeState(context.Background(), scope)
	if err != nil || state.Version != 0 || state.ChangeSeq != 0 {
		t.Fatalf("state after rejected mutation = %#v, error = %v", state, err)
	}
}

func TestCanonicalStoreRejectsDanglingHeadAtomically(t *testing.T) {
	store := openCanonicalTestStore(t, filepath.Join(t.TempDir(), "memory.badger"))

	t.Cleanup(func() { _ = store.Close() })

	scope := boyan.Scope{Key: "canonical:dangling-head", Kind: boyan.ScopeKindPersonal}

	mutation := boyan.CanonicalMutation{
		SchemaVersion: boyan.CanonicalSchemaVersionV1,
		Scope:         scope,
		Operation: boyan.OperationRecord{
			OperationID: "operation-1",
			Fingerprint: "fingerprint-1",
			CommittedAt: time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC),
		},
		Heads: []boyan.ItemHead{{ItemID: "item-1", RevisionID: "missing-revision"}},
	}
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err == nil {
		t.Fatal("dangling head mutation succeeded")
	}

	state, err := store.LoadScopeState(context.Background(), scope)
	if err != nil || state.Version != 0 {
		t.Fatalf("state after rejected dangling head = %#v, error = %v", state, err)
	}
}
