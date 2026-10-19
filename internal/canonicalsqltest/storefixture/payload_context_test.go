package storefixture_test

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Catches reference validation masking caller cancellation and rejected writes
// changing immutable bytes or archive stamps through either public SQL store.
func TestPublicPayloadContextPrecedence(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		store := f.Store.(privacyStore)
		probe := newMutationProbe(t, f)
		data := []byte("context precedence payload")

		ref := privacyRef("context-payload", data)
		if err := store.PutPayload(t.Context(), data, ref); err != nil {
			t.Fatal(err)
		}

		before := probe.stamps(t)
		for _, kind := range []string{"canceled", "deadline", "live", "nil"} {
			t.Run(kind, func(t *testing.T) {
				ctx, want := payloadCallerContext(t, kind)

				refs := map[string]memory.PayloadRef{
					"empty":          {},
					"missing-id":     {Digest: ref.Digest, ByteSize: ref.ByteSize},
					"missing-digest": {ID: ref.ID, ByteSize: ref.ByteSize},
					"malformed-id":   {ID: "bad\tid", Digest: ref.Digest, ByteSize: ref.ByteSize},
					"zero-size":      {ID: ref.ID, Digest: ref.Digest},
				}
				if kind != "live" {
					refs["valid"] = ref
				}

				for name, candidate := range refs {
					t.Run(name, func(t *testing.T) {
						assertPayloadCallerErrors(t, store, ctx, data, candidate, want)
					})
				}
			})
		}

		if after := probe.stamps(t); !reflect.DeepEqual(after, before) {
			t.Fatal("rejected payload operations changed archive stamps")
		}

		got, err := store.LoadPayload(t.Context(), ref)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("rejected operations changed payload: %q %v", got, err)
		}

		assertPayloadDeletedStamp(t, store, probe, ref, before)
	})
}

func payloadCallerContext(t *testing.T, kind string) (context.Context, error) {
	t.Helper()

	switch kind {
	case "canceled":
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		return ctx, context.Canceled
	case "deadline":
		ctx, cancel := context.WithDeadline(t.Context(), time.Unix(1, 0))
		t.Cleanup(cancel)

		return ctx, context.DeadlineExceeded
	case "nil":
		return nil, nil
	default:
		return t.Context(), nil
	}
}

func assertPayloadCallerErrors(t *testing.T, store privacyStore, ctx context.Context,
	data []byte, ref memory.PayloadRef, want error) {
	t.Helper()

	operations := map[string]func() error{
		"put": func() error { return store.PutPayload(ctx, data, ref) },
		"load": func() error {
			got, err := store.LoadPayload(ctx, ref)
			if got != nil {
				t.Error("rejected load exposed payload bytes")
			}

			return err
		},
		"delete": func() error { return store.DeletePayload(ctx, ref) },
	}
	for name, operation := range operations {
		t.Run(name, func(t *testing.T) {
			err := operation()
			if want != nil {
				if !errors.Is(err, want) {
					t.Fatalf("caller error=%v want=%v", err, want)
				}

				return
			}

			code, class, ok := memory.ClassifyError(err)
			if !ok || code != memory.CodeInvalidDerived || class != memory.ErrorClassPermanent {
				t.Fatalf("validation error=%v code/class=%s/%s", err, code, class)
			}
		})
	}
}

func assertPayloadDeletedStamp(t *testing.T, store privacyStore, probe mutationProbe,
	ref memory.PayloadRef, before []canonicalsql.Stamp) {
	t.Helper()

	if err := store.DeletePayload(t.Context(), ref); err != nil {
		t.Fatal(err)
	}

	got, err := store.LoadPayload(t.Context(), ref)
	if got != nil {
		t.Fatal("deleted payload exposed bytes")
	}

	assertMutationCode(t, err, memory.CodeNotFound)

	after := probe.stamps(t)
	if len(before) != 1 || len(after) != 1 || !after[0].Deleted ||
		after[0].Cursor.Sequence <= before[0].Cursor.Sequence || after[0].Key.Family != canonicalsql.Payload {
		t.Fatalf("delete did not replace payload stamp with an advanced tombstone: %+v", after)
	}
}
