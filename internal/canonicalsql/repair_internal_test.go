package canonicalsql

import (
	"bytes"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestRepairStampPagesAndIntegrity(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		keys := []Key{
			{Family: Source, Scope: testScope(), Parts: [][]byte{[]byte("aa")}},
			{Family: Source, Scope: testScope(), Parts: [][]byte{[]byte("b")}},
			{Family: Payload, Parts: [][]byte{[]byte("a\x00:")}},
		}

		mustUpdate(t, c, func(tx *Tx) error {
			for _, key := range keys {
				if err := tx.Put(t.Context(), key, envelope(t, key.Family, true)); err != nil {
					return err
				}
			}

			return nil
		})

		assertInitialStampPages(t, c, len(keys))

		mustUpdate(t, c, func(tx *Tx) error { return tx.Delete(t.Context(), keys[0]) })
		mustUpdate(t, c, func(tx *Tx) error { return tx.Delete(t.Context(), keys[0]) })
		mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), keys[0], envelope(t, Source, false)) })
		assertClock(t, c, 4, 3)

		if err := c.View(t.Context(), func(tx *Tx) error { return tx.CheckIntegrity(t.Context()) }); err != nil {
			t.Fatal(err)
		}
	})
}

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

func assertInitialStampPages(t *testing.T, c *Connection, count int) {
	t.Helper()

	if err := c.View(t.Context(), func(tx *Tx) error {
		var (
			after *StampCursor
			seen  []Stamp
		)

		for {
			page, err := tx.ScanStamps(t.Context(), after, 1)
			if err != nil {
				return err
			}

			if len(page) == 0 {
				break
			}

			seen = append(seen, page[0])
			cursor := page[0].Cursor
			after = &cursor
		}

		if len(seen) != count {
			t.Fatalf("stamp page omissions: %d", len(seen))
		}

		for _, stamp := range seen {
			if stamp.Cursor.Sequence != 1 || stamp.Deleted || len(stamp.Envelope) == 0 {
				t.Fatal("invalid live stamp")
			}
		}

		return tx.CheckIntegrity(t.Context())
	}); err != nil {
		t.Fatal(err)
	}
}
