//go:build integration

package canonicalsql

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Replay a real selected SQL page, rather than manufacturing hydrated records.
// The collector must reject progress regression even if every row is valid.
type boundaryPageFault struct {
	base           query
	mode           string
	cancel         context.CancelFunc
	pages          int
	firstStatement string
	firstArgs      []any
}

func (q *boundaryPageFault) exec(ctx context.Context, statement string, args ...any) (int64, error) {
	return q.base.exec(ctx, statement, args...)
}

func (q *boundaryPageFault) query(ctx context.Context, statement string, args ...any) (rows, error) {
	if !strings.Contains(statement, "FROM head ") && !strings.Contains(statement, "FROM boyan_canonical.head ") {
		return q.base.query(ctx, statement, args...)
	}

	q.pages++
	if q.pages == 1 {
		q.firstStatement = statement

		q.firstArgs = append([]any(nil), args...)
	}

	if q.pages == 2 {
		switch q.mode {
		case "repeated-page":
			statement, args = q.firstStatement, q.firstArgs
		case "canceled-page":
			q.cancel()
		}
	}

	return q.base.query(ctx, statement, args...)
}

func TestBoundaryCollectorNativePageFaults(t *testing.T) {
	for _, mode := range []string{"valid", "repeated-page", "canceled-page"} {
		t.Run(mode, func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
				ids := make([]string, 257)
				for i := range ids {
					ids[i] = fmt.Sprintf("item-%05d", i)
				}

				scope := testScope()
				seedActivePage(t, owner, scope, ids)

				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()

				var got []string

				var probe *boundaryPageFault

				err := owner.View(ctx, func(ctx context.Context, tx *Tx) error {
					probe = &boundaryPageFault{base: tx.q, mode: mode, cancel: cancel}
					tx.q = probe

					var err error

					got, err = collectBoundaryRevisionIDs(ctx, tx, scope, memory.MaxSnapshotItems)

					return err
				})
				assertBoundaryPageFault(t, mode, got, err, probe)
			})
		})
	}
}

func assertBoundaryPageFault(t *testing.T, mode string, got []string, err error, probe *boundaryPageFault) {
	t.Helper()

	if probe == nil || probe.pages < 2 {
		t.Fatal("fixture did not cross a real SQL page boundary")
	}

	switch mode {
	case "valid":
		assertValidBoundaryPage(t, got, err)
	case "repeated-page":
		assertCode(t, err, memory.CodeStoreFailure)
	case "canceled-page":
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("native page cancellation lost: %v", err)
		}
	}

	if mode != "valid" && len(got) != 0 {
		t.Fatal("failed collector returned partial model input")
	}
}

func assertValidBoundaryPage(t *testing.T, got []string, err error) {
	t.Helper()

	if err != nil || len(got) != 257 || got[256] != "item-00256" {
		t.Fatalf("valid native collector: %d %v", len(got), err)
	}
}
