package storefixture_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Catches overbroad absence conversion: corruption, payload failure, readiness,
// cancellation and erase fences are errors, not permission to replay fallback.
func TestPublicBoundaryCompatibilityErrorPropagation(t *testing.T) {
	for _, kind := range []string{"payload-missing", "payload-corrupt", "head-corrupt", "source-binding",
		"unprepared", "scope-fence", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, p mutationProbe) {
				m := compatibleTraceMutation(t)
				if _, err := store.ApplyCanonicalMutation(t.Context(), m); err != nil {
					t.Fatal(err)
				}

				want := alterCompatibilityError(t, p, m, kind)

				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()

				if kind == "canceled" {
					cancel()
				}

				r := publicTraceReader(t, f.Store).(boundaryAuxReader)

				got, found, err := r.LoadCanonicalCompatibility(ctx, m.Scope, m.Revisions[0].RevisionID)
				if found || !reflect.DeepEqual(got, memory.CanonicalCompatibilityPayload{}) {
					t.Fatalf("error exposed compatibility: %+v found=%v err=%v", got, found, err)
				}

				assertCompatibilityError(t, kind, want, err)
			})
		})
	}
}

func alterCompatibilityError(t *testing.T, p mutationProbe, m memory.CanonicalMutation,
	kind string) memory.ErrorCode {
	t.Helper()

	if kind == "head-corrupt" {
		p.write(t, canonicalsql.Key{Family: canonicalsql.Head, Scope: m.Scope,
			Parts: [][]byte{[]byte(m.Items[0].ItemID)}},
			memory.ItemHead{ItemID: "other", RevisionID: m.Revisions[0].RevisionID})

		return memory.CodeStoreFailure
	}

	want := alterReaderAuthority(t, p, m, kind)
	if kind == "payload-missing" {
		want = memory.CodeNotFound
	}

	return want
}

func assertCompatibilityError(t *testing.T, kind string, want memory.ErrorCode, err error) {
	t.Helper()

	if kind == "canceled" {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation identity lost: %v", err)
		}

		return
	}

	wantClass := memory.ErrorClassPermanent
	if kind == "unprepared" || kind == "scope-fence" {
		wantClass = memory.ErrorClassRetryable
	}

	code, class, ok := memory.ClassifyError(err)
	if !ok || code != want || class != wantClass {
		t.Fatalf("error=%v code/class=%s/%s want=%s/%s", err, code, class, want, wantClass)
	}
}
