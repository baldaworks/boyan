package canonicalsql

import (
	"bytes"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestRepairSecondaryAndLegacyReads(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		scope := testScope()
		long := deterministicLongID(65535)

		mustUpdate(t, c, func(tx *Tx) error {
			for _, id := range []string{"aa", "b", "b\x00"} {
				key := Key{Family: Message, Scope: scope, Parts: [][]byte{[]byte(id)}}

				value := memory.MessageRecord{MessageID: id, SourceID: long}
				if err := tx.Put(t.Context(), key, envelope(t, Message, value)); err != nil {
					return err
				}
			}

			id := []byte("boyan-derived:v1:operation:" + strings.Repeat("!", 64))
			key := Key{Family: Operation, Scope: scope, Parts: [][]byte{id}}

			return tx.Put(t.Context(), key, envelope(t, Operation, true))
		})

		if err := c.View(t.Context(), func(tx *Tx) error {
			page, err := tx.MessagesBySource(t.Context(), scope, []byte(long), nil, 1)
			if err != nil {
				return err
			}

			if len(page) != 1 || !bytes.Equal(page[0].Key.Parts[0], []byte("aa")) {
				t.Fatal("secondary raw ordering")
			}

			found, err := tx.HasLegacyOperationNamespace(t.Context(), scope)
			if err == nil && !found {
				t.Fatal("malformed suffix hidden")
			}

			return err
		}); err != nil {
			t.Fatal(err)
		}
	})
}
