//go:build integration

package canonicalsql

import (
	"bytes"
	"context"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestPrivacyPayloadGatesAndStamps(t *testing.T) {
	for _, kind := range []string{"guard", "lock", "erased", "malformed-lock", "malformed-erased", "immutable", "size"} {
		t.Run(kind, func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
				runPayloadGate(t, owner, kind)
			})
		})
	}
}

func TestPrivacyDenialExactScopeAndLoadingFence(t *testing.T) {
	forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
		scope := memory.Scope{Key: "test:denial", Kind: memory.ScopeKindPersonal}

		if err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
			for _, family := range []Family{DeniedSource, DeniedRevision} {
				value := any(deniedSource{SourceID: "id", DeniedAt: time.Now().UTC()})
				if family == DeniedRevision {
					value = deniedRevision{RevisionID: "id", DeniedAt: time.Now().UTC()}
				}

				key, _ := privacyKey(scope, family, "id")
				if err := putPrivacyRecord(ctx, tx, key, family, value); err != nil {
					return err
				}
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}

		assertPrivacyDenials(t, owner, scope)
	})
}

func runPayloadGate(t *testing.T, owner *Owner, kind string) {
	t.Helper()

	data := []byte("payload")

	ref := privacyRef("payload", data)
	if err := owner.PutPayload(t.Context(), data, ref); err != nil {
		t.Fatal(err)
	}

	seedPayloadGate(t, owner, ref, kind)

	attempt, attemptRef := data, ref
	if kind == "immutable" {
		attempt = []byte("different")
		attemptRef = privacyRef(ref.ID, attempt)
	}

	if kind == "size" {
		attempt = bytes.Repeat([]byte("x"), MaxEnvelopeBytes+1)
		attemptRef = privacyRef("oversize", attempt)
	}

	if err := owner.PutPayload(t.Context(), attempt, attemptRef); err == nil {
		t.Fatal("payload gate bypassed")
	}

	assertPayloadDeleteGate(t, owner, ref, kind)

	got, err := owner.LoadPayload(t.Context(), ref)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("failed write changed content: %v", err)
	}
}

func seedPayloadGate(t *testing.T, owner *Owner, ref memory.PayloadRef, kind string) {
	t.Helper()

	if err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		family := EraseBlobLock
		value := any(map[string]any{"scope": memory.Scope{Key: "test:scope",
			Kind: memory.ScopeKindPersonal},
			"operation_id": "erase"})
		key := Key{Family: family, Parts: [][]byte{[]byte(ref.ID)}}

		switch kind {
		case "guard":
			family = EraseGuard
			key = Key{Family: family}
			value = map[string]any{"pending": 1}
		case "erased":
			family = ErasedBlob
			key.Family = family
			value = true
		case "malformed-erased":
			family = ErasedBlob
			key.Family = family
			value = false
		case "malformed-lock":
			value = map[string]any{"operation_id": ""}
		case "immutable", "size":
			return nil
		}

		return putPrivacyRecord(ctx, tx, key, family, value)
	}); err != nil {
		t.Fatal(err)
	}
}
func assertPrivacyDenials(t *testing.T, owner *Owner, scope memory.Scope) {
	t.Helper()

	for _, check := range []func(context.Context,
		memory.Scope,
		string) (bool,
		error){owner.IsSourceDenied,
		owner.IsRevisionDenied} {
		denied, err := check(t.Context(), scope, "id")
		if err != nil || !denied {
			t.Fatalf("missing denial: %v", err)
		}

		other := scope
		other.Kind = memory.ScopeKindGroup

		denied, err = check(t.Context(), other, "id")
		if err != nil || denied {
			t.Fatalf("scope leak: %v", err)
		}
	}
}

func assertPayloadDeleteGate(t *testing.T, owner *Owner, ref memory.PayloadRef, kind string) {
	t.Helper()

	if kind == "guard" || kind == "lock" || kind == "malformed-lock" {
		if err := owner.DeletePayload(t.Context(), ref); err == nil {
			t.Fatal("delete bypassed gate")
		}
	}
}
