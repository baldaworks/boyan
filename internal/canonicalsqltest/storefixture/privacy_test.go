package storefixture_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

type privacyStore interface {
	PrepareSourcePrivacy(ctx context.Context) error
	SourcePrivacyReady(ctx context.Context) error
	PutPayload(ctx context.Context, data []byte, ref memory.PayloadRef) error
	LoadPayload(ctx context.Context, ref memory.PayloadRef) ([]byte, error)
	DeletePayload(ctx context.Context, ref memory.PayloadRef) error
	IsSourceDenied(ctx context.Context, scope memory.Scope, id string) (bool, error)
	IsRevisionDenied(ctx context.Context, scope memory.Scope, id string) (bool, error)
}

func TestPublicPrivacyAndPayloadContract(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, fixture *storefixture.Fixture) {
		store, ok := fixture.Store.(privacyStore)
		if !ok {
			t.Fatal("public store lacks implemented privacy and payload authority")
		}

		if err := store.PrepareSourcePrivacy(t.Context()); err != nil {
			t.Fatal(err)
		}

		if err := store.SourcePrivacyReady(t.Context()); err != nil {
			t.Fatal(err)
		}

		data := []byte("durable raw content")
		digest := sha256.Sum256(data)

		ref := memory.PayloadRef{ID: "public-payload", Digest: hex.EncodeToString(digest[:]), ByteSize: uint32(len(data))}
		if err := store.PutPayload(t.Context(), data, ref); err != nil {
			t.Fatal(err)
		}

		if err := fixture.Store.Close(); err != nil {
			t.Fatal(err)
		}

		reopened, err := fixture.Open(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() {
			if err := reopened.Close(); err != nil {
				t.Error(err)
			}
		})

		store = reopened.(privacyStore)

		got, err := store.LoadPayload(t.Context(), ref)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("payload lost on reopen: %q %v", got, err)
		}

		assertPublicPrivacyOperations(t, store, ref)
	})
}

func assertPublicPrivacyOperations(t *testing.T, store privacyStore, ref memory.PayloadRef) {
	t.Helper()

	scope := memory.Scope{Key: "test:public", Kind: memory.ScopeKindPersonal}
	for _, check := range []func(context.Context,
		memory.Scope,
		string) (bool,
		error){store.IsSourceDenied,
		store.IsRevisionDenied} {
		if denied, err := check(t.Context(), scope, "absent"); err != nil || denied {
			t.Fatalf("absent denial: %v %v", denied, err)
		}
	}

	if err := store.DeletePayload(t.Context(), ref); err != nil {
		t.Fatal(err)
	}

	if _, err := store.LoadPayload(t.Context(), ref); err == nil {
		t.Fatal("deleted payload returned")
	}
}
