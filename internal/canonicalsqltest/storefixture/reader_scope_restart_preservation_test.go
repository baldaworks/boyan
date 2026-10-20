package storefixture_test

import (
	"context"
	"errors"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestPublicScopePagesResumeCursorAfterReopen(t *testing.T) {
	forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, _ mutationProbe) {
		scopes := []memory.Scope{
			{Key: "startup:other", Kind: memory.ScopeKindPersonal},
			{Key: "startup:same", Kind: memory.ScopeKindGroup},
			{Key: "startup:same", Kind: memory.ScopeKindPersonal},
		}
		for _, scope := range scopes {
			mutation := nativeMutation(t)

			mutation.Scope = scope
			if _, err := writeBoundary(t, store, mutation, true); err != nil {
				t.Fatal(err)
			}
		}

		first, cursor, err := f.Store.(activeReaderStore).ListCanonicalScopeStates(t.Context(), nil, 1)
		if err != nil || len(first) != 1 || first[0].Scope != scopes[0] || len(cursor) == 0 {
			t.Fatalf("first scope page=%+v cursor=%x err=%v", first, cursor, err)
		}

		reopened := reopenPreservedOwner(t, f).(activeReaderStore)
		assertResumedScopePages(t, reopened, cursor, scopes[1:])
		assertScopePagerBounds(t, reopened)
	})
}

func assertResumedScopePages(t *testing.T, store activeReaderStore, cursor []byte, scopes []memory.Scope) {
	t.Helper()

	for i, scope := range scopes {
		page, next, err := store.ListCanonicalScopeStates(t.Context(), cursor, 1)
		if err != nil || len(page) != 1 || page[0].Scope != scope {
			t.Fatalf("reopened exclusive scope page=%+v want=%+v err=%v", page, scope, err)
		}

		if (len(next) == 0) != (i == len(scopes)-1) {
			t.Fatalf("reopened scope page %d wrong terminal cursor=%x", i, next)
		}

		cursor = next
	}
}

func assertScopePagerBounds(t *testing.T, store activeReaderStore) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	page, cursor, err := store.ListCanonicalScopeStates(ctx, nil, 1)
	if !errors.Is(err, context.Canceled) || len(page) != 0 || len(cursor) != 0 {
		t.Fatalf("canceled scope pager: page=%+v cursor=%x err=%v", page, cursor, err)
	}

	if _, _, err := store.ListCanonicalScopeStates(t.Context(), nil, memory.MaxCanonicalReadRecords+1); err == nil {
		t.Fatal("scope pager accepted excessive limit")
	}
}
