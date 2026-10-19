package canonicalsql

import (
	"context"
	"errors"
	"strings"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Only the actual head query is faulted; admission and other native reads run.
type readerHeadFaultQuery struct {
	base  query
	cause error
}

func (q readerHeadFaultQuery) exec(ctx context.Context, statement string, args ...any) (int64, error) {
	return q.base.exec(ctx, statement, args...)
}

func (q readerHeadFaultQuery) query(ctx context.Context, statement string, args ...any) (rows, error) {
	if strings.Contains(statement, "envelope") && (strings.Contains(statement, "FROM head ") ||
		strings.Contains(statement, "FROM boyan_canonical.head ")) {
		return nil, q.cause
	}

	return q.base.query(ctx, statement, args...)
}

func TestReaderHeadRecheckPreservesTechnicalErrors(t *testing.T) {
	forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
		for _, tc := range []struct {
			name  string
			cause error
		}{
			{"store-failure", errors.New("private driver detail")},
			{"canceled", context.Canceled},
			{"deadline", context.DeadlineExceeded},
		} {
			t.Run(tc.name, func(t *testing.T) {
				captured := readerSnapshot{state: memory.ScopeState{Scope: testScope()},
					records: []memory.RecallRecord{{ItemID: "item", RevisionID: "revision"}}}

				err := owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
					tx.q = readerHeadFaultQuery{base: tx.q, cause: tc.cause}

					return recheckReaderRevisions(ctx, tx, captured)
				})
				if tc.name == "store-failure" {
					code, class, _ := memory.ClassifyError(err)
					if code != memory.CodeStoreFailure || class != memory.ErrorClassPermanent ||
						strings.Contains(err.Error(), "private driver detail") {
						t.Fatalf("head technical error=%v (%s/%s)", err, code, class)
					}
				} else if !errors.Is(err, tc.cause) {
					t.Fatalf("head error=%v want cause=%v", err, tc.cause)
				}
			})
		}
	})
}

func TestReaderHeadRecheckDetectsOnlyActualChange(t *testing.T) {
	for _, kind := range []string{"unchanged", "changed", "missing", "corrupt", "wrong-item", "invalid-head"} {
		t.Run(kind, func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
				seedReaderHeadRecheck(t, owner, kind)

				captured := readerSnapshot{state: memory.ScopeState{Scope: testScope()},
					records: []memory.RecallRecord{{ItemID: "item", RevisionID: "revision"}}}

				err := owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
					return recheckReaderRevisions(ctx, tx, captured)
				})
				assertReaderHeadRecheckResult(t, kind, err)
			})
		})
	}
}

func seedReaderHeadRecheck(t *testing.T, owner *Owner, kind string) {
	t.Helper()

	key := Key{Family: Head, Scope: testScope(), Parts: [][]byte{[]byte("item")}}

	if kind != "missing" {
		revision := "revision"
		if kind == "changed" {
			revision = "replacement"
		}

		if err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
			return tx.Put(ctx, key, envelope(t, Head, memory.ItemHead{ItemID: "item", RevisionID: revision}))
		}); err != nil {
			t.Fatal(err)
		}
	}

	if kind == "corrupt" {
		rawExec(t, owner.connection, "UPDATE "+owner.connection.table("head")+" SET envelope=$1", []byte("corrupt"))
	}

	if kind == "wrong-item" || kind == "invalid-head" {
		head := memory.ItemHead{ItemID: "other", RevisionID: "revision"}
		if kind == "invalid-head" {
			head = memory.ItemHead{ItemID: "item"}
		}

		rawExec(t, owner.connection, "UPDATE "+owner.connection.table("head")+" SET envelope=$1",
			envelope(t, Head, head))
	}
}

func assertReaderHeadRecheckResult(t *testing.T, kind string, err error) {
	t.Helper()

	if kind == "unchanged" {
		if err != nil {
			t.Fatal(err)
		}

		return
	}

	code, _, _ := memory.ClassifyError(err)
	if kind == "corrupt" || kind == "wrong-item" || kind == "invalid-head" {
		assertReaderHeadCorruption(t, err)
	} else if code != memory.CodeConflict || !memory.IsRetryable(err) {
		t.Fatalf("changed/missing head=%v", err)
	}
}

func assertReaderHeadCorruption(t *testing.T, err error) {
	t.Helper()

	code, _, _ := memory.ClassifyError(err)
	if code == memory.CodeConflict || err == nil || memory.IsRetryable(err) {
		t.Fatalf("corruption became retry conflict: %v", err)
	}
}
