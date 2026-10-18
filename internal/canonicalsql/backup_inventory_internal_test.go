package canonicalsql

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Independent shapes from TASK-002, executable Put/Delete/absent-delete/reinsert
// for every durable family. Domain values are intentionally structural fixtures;
// public writer coverage is recorded separately from this primitive inventory.
//
//nolint:cyclop,gocognit,funlen // Full family lifecycle checks share the same transaction/stamp history.
func TestBackupEveryFamilyWriterStampInventory(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		if len(expectedShapes) != 38 {
			t.Fatal("writer inventory incomplete")
		}

		for _, shape := range expectedShapes {
			testFamilyRoundtrip(t, c, shape.family)
		}

		if err := c.View(
			t.Context(),
			func(tx *Tx) error { return tx.checkIntegrity(t.Context(), 1) },
		); err != nil {
			t.Fatal(err)
		}

		for _, deleted := range []bool{true, true, false, true} {
			mustUpdate(t, c, func(tx *Tx) error {
				for _, shape := range expectedShapes {
					key := familyFixtureKey(t, shape.family)
					if deleted {
						if err := tx.Delete(t.Context(), key); err != nil {
							return err
						}

						continue
					}

					var value any = true

					switch shape.family {
					case ScopeState:
						value = memory.ScopeState{
							SchemaVersion: memory.CanonicalSchemaVersionV1,
							Scope:         key.Scope,
							Version:       1,
							ChangeSeq:     1,
						}
					case Message:
						value = memory.MessageRecord{
							MessageID: string(key.Parts[0]),
							SourceID:  "source",
						}
					case Revision:
						value = memory.MemoryRevision{
							RevisionID: string(key.Parts[0]),
							ItemID:     "item",
							Revision:   1,
						}
					}

					if err := tx.Put(
						t.Context(),
						key,
						envelope(t, shape.family, value),
					); err != nil {
						return err
					}
				}

				return nil
			})

			if err := c.View(t.Context(), func(tx *Tx) error {
				var after *StampCursor
				for range 38 {
					page, err := tx.ScanStamps(t.Context(), after, 1)
					if err != nil {
						return err
					}

					if len(page) != 1 || page[0].Deleted != deleted {
						t.Fatalf("stamp=%+v deleted=%t", page, deleted)
					}

					cursor := page[0].Cursor
					after = &cursor
				}

				page, err := tx.ScanStamps(t.Context(), after, 1)
				if len(page) != 0 {
					t.Fatal("duplicate logical stamp")
				}

				return err
			}); err != nil {
				t.Fatal(err)
			}
		}

		if err := c.View(t.Context(), func(tx *Tx) error {
			for _, since := range []uint64{0, 41} {
				var out bytes.Buffer

				end, err := tx.writeCanonicalBackup(t.Context(), &out, since, 1)
				if err != nil {
					return err
				}

				if end != 42 || countBackupRecords(t, out.Bytes()) != 38 {
					t.Fatalf("inventory end=%d", end)
				}
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
}

func countBackupRecords(t *testing.T, data []byte) int {
	t.Helper()

	count := 0

	for len(data) > 0 {
		if len(data) < 4 {
			t.Fatal("truncated frame")
		}

		size := int(binary.BigEndian.Uint32(data))

		data = data[4:]
		if size > len(data) {
			t.Fatal("truncated body")
		}

		var record struct {
			Kind     string
			Deleted  bool
			Envelope []byte
		}
		if err := json.Unmarshal(data[:size], &record); err != nil {
			t.Fatal(err)
		}

		if record.Kind == "record" {
			count++
		}

		data = data[size:]
	}

	return count
}

//nolint:cyclop,gocognit // Full/delete/reinsert snapshots retain one exact cursor chain at page size one.
func TestBackupPageOneLongCompoundAndNativeHashCollision(t *testing.T) {
	forEachOwner(t, func(t *testing.T, o *Owner, _ func() *Owner) {
		long := deterministicLongID(math.MaxUint16)
		scope := memory.Scope{
			Key:  "test:" + deterministicLongID(math.MaxUint16-5),
			Kind: memory.ScopeKindPersonal,
		}
		keys := []Key{
			{Family: Payload, Parts: [][]byte{[]byte("10187")}},
			{Family: Payload, Parts: [][]byte{[]byte("159793")}},
			{Family: ProjectionManifest, Scope: scope, Parts: [][]byte{[]byte(long), []byte(long)}},
			{
				Family: ProjectionManifest,
				Scope:  scope,
				Parts:  [][]byte{[]byte(long), []byte(long[:len(long)-1])},
			},
		}

		for step := uint64(0); step < 3; step++ {
			if err := o.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
				for _, key := range keys {
					if step == 1 {
						if err := tx.Delete(ctx, key); err != nil {
							return err
						}

						continue
					}

					if err := tx.Put(ctx, key, envelope(t, key.Family, "value")); err != nil {
						return err
					}
				}

				return nil
			}); err != nil {
				t.Fatal(err)
			}

			if err := o.View(t.Context(), func(ctx context.Context, tx *Tx) error {
				var out bytes.Buffer

				end, err := tx.writeCanonicalBackup(ctx, &out, step, 1)
				if err == nil && (end != step+1 || countBackupRecords(t, out.Bytes()) != 4) {
					t.Fatal("page one omitted or repeated identity")
				}

				return err
			}); err != nil {
				t.Fatal(err)
			}
		}
	})
}
