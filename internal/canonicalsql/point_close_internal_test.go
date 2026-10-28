package canonicalsql

import (
	"context"
	"errors"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

type failingPointRows struct{}

func (failingPointRows) Next() bool        { return true }
func (failingPointRows) Scan(...any) error { return limitError() }
func (failingPointRows) Err() error        { return nil }
func (failingPointRows) Close() error      { return context.Canceled }

type failingPointQuery struct{}

func (failingPointQuery) exec(context.Context, string, ...any) (int64, error) { return 0, nil }
func (failingPointQuery) query(context.Context, string, ...any) (rows, error) {
	return failingPointRows{}, nil
}

func TestPointReadPreservesCloseFailure(t *testing.T) {
	for _, postgres := range []bool{false, true} {
		t.Run(map[bool]string{false: "sqlite", true: "postgres"}[postgres], func(t *testing.T) {
			tx := &Tx{connection: &Connection{}, q: failingPointQuery{}}

			var err error
			if postgres {
				_, err = tx.resolvePoint(t.Context(), Key{}, familyDef{table: "test"}, nil)
			} else {
				_, err = tx.getPoint(t.Context(), Key{}, familyDef{table: "test"}, nil)
			}

			if !errors.Is(err, context.Canceled) || !errors.Is(tx.failed, context.Canceled) {
				t.Fatalf("close failure lost: returned %v, transaction %v", err, tx.failed)
			}

			if code, _, ok := memory.ClassifyError(err); !ok || code != memory.CodeLimitExceeded {
				t.Fatalf("original read failure lost: %v", err)
			}
		})
	}
}
