//go:build integration

package canonicalsql

import (
	"bytes"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Stored corruption must fail identically through point, page and public reads;
// malformed caller input must retain its own classification.
func TestStoredRevisionCorruptionClassification(t *testing.T) {
	for _, fault := range []string{"revision_id", "item_id", "invalid_item_id", "envelope"} {
		t.Run(fault, func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
				h := prepareTraceHistory(t, owner)
				for _, operation := range []string{"get", "scan", "secondary", "active", "boundary"} {
					if err := readStoredRevision(t, owner, h, operation, false); err != nil {
						t.Fatal(err)
					}
				}

				revision := h.revision

				switch fault {
				case "revision_id":
					revision.RevisionID = "foreign-revision"
				case "item_id":
					revision.ItemID = "foreign-item"
				case "invalid_item_id":
					revision.ItemID = ""
				}

				encoded := envelope(t, Revision, revision)
				if fault == "envelope" {
					encoded = []byte("private-corrupt-envelope")
				}

				rawExec(t, owner.connection, "UPDATE "+owner.connection.table("revision")+
					" SET envelope=$1 WHERE id=$2", encoded, []byte(h.revision.RevisionID))

				for _, operation := range []string{"get", "scan", "secondary", "active", "boundary"} {
					t.Run(operation, func(t *testing.T) {
						err := readStoredRevision(t, owner, h, operation, true)
						assertPermanentStoredCode(t, err, memory.CodeStoreFailure)

						if bytes.Contains([]byte(err.Error()), []byte("private-corrupt-envelope")) {
							t.Fatal("stored payload leaked in error")
						}
					})
				}
			})
		})
	}
}

func readStoredRevision(t *testing.T, owner *Owner, h privacyHistory, operation string, corrupt bool) error {
	t.Helper()

	switch operation {
	case "boundary":
		return readStoredRevisionBoundary(t, owner, h, corrupt)
	case "active":
		records, err := owner.LoadActiveRecallRecords(t.Context(), h.scope, 10)
		if corrupt && len(records) != 0 || !corrupt && len(records) != 1 {
			t.Fatalf("active read returned %d records", len(records))
		}

		return err
	}

	return readStoredRevisionPrimitive(t, owner.connection, h, operation, corrupt)
}

func readStoredRevisionBoundary(t *testing.T, owner *Owner, h privacyHistory, corrupt bool) error {
	t.Helper()

	view, err := owner.LoadCanonicalBoundaryView(t.Context(), h.scope)
	if corrupt && (len(view.LegacyToCanonical) != 0 || len(view.Snapshot.Atoms) != 0) {
		t.Fatal("corrupt boundary exposed partial model input")
	}

	if !corrupt && len(view.LegacyToCanonical) != 1 {
		t.Fatal("valid boundary fixture missing")
	}

	return err
}

func readStoredRevisionPrimitive(t *testing.T, c *Connection, h privacyHistory, operation string, corrupt bool) error {
	t.Helper()

	return c.View(t.Context(), func(tx *Tx) error {
		var (
			count int
			err   error
		)

		switch operation {
		case "get":
			var encoded []byte

			encoded, err = tx.Get(t.Context(), Key{Family: Revision, Scope: h.scope,
				Parts: [][]byte{[]byte(h.revision.RevisionID)}})
			count = len(encoded)
		case "scan", "secondary":
			var rows []Row

			if operation == "scan" {
				rows, err = tx.Scan(t.Context(), Range{Family: Revision, Scope: &h.scope, Limit: 10})
			} else {
				rows, err = tx.RevisionsByItem(t.Context(), h.scope, []byte(h.item.ItemID), nil, 10)
			}

			count = len(rows)
		}

		if corrupt {
			assertPermanentStoredCode(t, err, memory.CodeStoreFailure)

			if count != 0 {
				t.Fatal("corrupt primitive exposed partial data")
			}

			return nil // Swallowing a failed read must still poison the transaction.
		}

		if count == 0 {
			t.Fatal("valid primitive fixture missing")
		}

		return err
	})
}

func TestStoredScanKeyCorruptionClassification(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		key := Key{Family: Source, Scope: testScope(), Parts: [][]byte{[]byte("source")}}

		mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), key, envelope(t, Source, true)) })

		rawExec(t, c, "UPDATE "+c.table("source")+" SET scope_kind=$1", []byte("invalid-kind"))

		err := c.View(t.Context(), func(tx *Tx) error {
			rows, err := tx.Scan(t.Context(), Range{Family: Source, Limit: 10})
			assertPermanentStoredCode(t, err, memory.CodeStoreFailure)

			if len(rows) != 0 {
				t.Fatal("corrupt stored key exposed rows")
			}

			return nil
		})
		assertPermanentStoredCode(t, err, memory.CodeStoreFailure)
	})
}

func TestStoredValidationKeepsCallerAndSizeErrors(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		key := Key{Family: Revision, Scope: testScope(), Parts: [][]byte{[]byte("revision")}}
		bad := envelope(t, Revision, memory.MemoryRevision{RevisionID: "foreign", ItemID: "item"})
		err := c.Update(t.Context(), func(tx *Tx) error { return tx.Put(t.Context(), key, bad) })
		assertPermanentStoredCode(t, err, memory.CodeInvalidDerived)

		err = c.View(t.Context(), func(tx *Tx) error {
			encoded, err := tx.Get(t.Context(), Key{Family: Revision, Scope: testScope()})
			if len(encoded) != 0 {
				t.Fatal("invalid caller key exposed data")
			}

			return err
		})
		assertPermanentStoredCode(t, err, memory.CodeInvalidDerived)
	})

	_, err := Encode(Revision, make(chan int))
	assertPermanentStoredCode(t, err, memory.CodeInvalidDerived)
	err = Decode(Revision, []byte("corrupt"), new(memory.MemoryRevision))
	assertPermanentStoredCode(t, err, memory.CodeInvalidDerived)
	_, err = validateStoredValue(Key{Family: Revision}, nil, MaxEnvelopeBytes+1, nil)
	assertPermanentStoredCode(t, err, memory.CodeLimitExceeded)
}

func assertPermanentStoredCode(t *testing.T, err error, want memory.ErrorCode) {
	t.Helper()

	code, class, ok := memory.ClassifyError(err)
	if !ok || code != want || class != memory.ErrorClassPermanent {
		t.Fatalf("error=%v classification=%s/%s, want %s/permanent", err, code, class, want)
	}
}
