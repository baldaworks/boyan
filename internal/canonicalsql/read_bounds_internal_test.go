package canonicalsql

import (
	"context"
	"errors"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestRepairReadBoundsAndContext(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		for _, limit := range []int{0, memory.MaxCanonicalReadRecords + 1} {
			err := c.View(t.Context(), func(tx *Tx) error {
				_, err := tx.ScanStamps(t.Context(), nil, limit)

				return err
			})
			assertCode(t, err, memory.CodeLimitExceeded)
		}

		err := c.View(t.Context(), func(tx *Tx) error {
			_, err := tx.ScanStamps(t.Context(), &StampCursor{Family: Source, BinaryKey: []byte{1}}, 1)

			return err
		})
		assertCode(t, err, memory.CodeInvalidDerived)
		err = c.View(t.Context(), func(tx *Tx) error {
			_, err := tx.RevisionsByItem(t.Context(), testScope(), []byte("item"), &RevisionCursor{}, 1)

			return err
		})
		assertCode(t, err, memory.CodeInvalidDerived)

		if err := c.View(t.Context(), func(tx *Tx) error {
			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			_, err := tx.RevisionsByItem(ctx, testScope(), []byte("item"), &RevisionCursor{}, 1)
			if !errors.Is(err, context.Canceled) {
				t.Fatal("context lost behind cursor validation")
			}

			return err
		}); !errors.Is(err, context.Canceled) {
			t.Fatal("transaction lost context")
		}

		assertClock(t, c, 0, 0)
	})
}
