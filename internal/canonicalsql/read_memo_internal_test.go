package canonicalsql

import (
	"bytes"
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Repeating a validated immutable point in one snapshot must not repeat its
// physical query. Returned buffers must not let callers corrupt that snapshot.
func TestReadSnapshotReusesValidatedPoint(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		key := Key{Family: Payload, Parts: [][]byte{[]byte("read-only")}}
		want := envelope(t, Payload, "retained bytes")
		mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), key, want) })

		err := c.View(t.Context(), func(tx *Tx) error {
			count := &readerWorkQuery{base: tx.q}
			tx.q = count

			for range 3 {
				got, err := tx.Get(t.Context(), key)
				if err != nil {
					return err
				}

				if !bytes.Equal(got, want) {
					t.Fatal("caller mutation changed the next snapshot result")
				}

				got[0] ^= 0xff
			}

			if count.reads != 1 {
				t.Fatalf("repeated immutable point issued %d physical queries; want one", count.reads)
			}

			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestReadSnapshotMemoSeparatesTypedTuples(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		personal := memory.Scope{Key: "chat:scope", Kind: memory.ScopeKindPersonal}
		group := memory.Scope{Key: "chat:scope", Kind: memory.ScopeKindGroup}
		other := memory.Scope{Key: "chat:scope\x00other", Kind: memory.ScopeKindPersonal}

		keys := []Key{
			{Family: ProvenanceForward, Scope: personal, Parts: [][]byte{[]byte("a"), []byte("b\x00c")}},
			{Family: ProvenanceForward, Scope: personal, Parts: [][]byte{[]byte("a\x00b"), []byte("c")}},
			{Family: ProvenanceReverse, Scope: personal, Parts: [][]byte{[]byte("a"), []byte("b\x00c")}},
			{Family: ProvenanceForward, Scope: group, Parts: [][]byte{[]byte("a"), []byte("b\x00c")}},
			{Family: ProvenanceForward, Scope: other, Parts: [][]byte{[]byte("a"), []byte("b\x00c")}},
		}
		for i, key := range keys {
			mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), key, envelope(t, key.Family, i)) })
		}

		err := c.View(t.Context(), func(tx *Tx) error {
			for range 2 {
				for i, key := range keys {
					got, err := tx.Get(t.Context(), key)
					if err != nil {
						return err
					}

					if !bytes.Equal(got, envelope(t, key.Family, i)) {
						t.Fatalf("typed tuple %d aliased another snapshot entry", i)
					}
				}
			}

			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestReadSnapshotMemoNeverCrossesWriterOrView(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		key := Key{Family: Payload, Parts: [][]byte{[]byte("changing")}}
		old, next := envelope(t, Payload, "old"), envelope(t, Payload, "next")
		mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), key, old) })

		if err := c.View(t.Context(), func(tx *Tx) error {
			_, err := tx.Get(t.Context(), key)

			return err
		}); err != nil {
			t.Fatal(err)
		}

		mustUpdate(t, c, func(tx *Tx) error {
			if _, err := tx.Get(t.Context(), key); err != nil {
				return err
			}

			if err := tx.Put(t.Context(), key, next); err != nil {
				return err
			}

			got, err := tx.Get(t.Context(), key)
			if !bytes.Equal(got, next) {
				t.Fatal("writer read returned a cached pre-mutation value")
			}

			return err
		})

		if err := c.View(t.Context(), func(tx *Tx) error {
			got, err := tx.Get(t.Context(), key)
			if !bytes.Equal(got, next) {
				t.Fatal("fresh view returned a previous snapshot value")
			}

			return err
		}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestReadSnapshotMemoChecksCancellationAndPoison(t *testing.T) {
	for _, failure := range []string{"canceled", "invalid-key"} {
		t.Run(failure, func(t *testing.T) {
			canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
				c := testConnection(t, db)
				key := Key{Family: Payload, Parts: [][]byte{[]byte("cached")}}

				mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), key, envelope(t, Payload, "data")) })

				err := c.View(t.Context(), func(tx *Tx) error {
					if _, err := tx.Get(t.Context(), key); err != nil {
						return err
					}

					var rejected error

					if failure == "canceled" {
						ctx, cancel := context.WithCancel(t.Context())
						cancel()

						_, rejected = tx.Get(ctx, key)
						if !errors.Is(rejected, context.Canceled) {
							t.Fatalf("cached hit lost cancellation: %v", rejected)
						}
					} else {
						_, rejected = tx.Get(t.Context(), Key{Family: "invalid"})
						assertCode(t, rejected, memory.CodeInvalidDerived)
					}

					_, after := tx.Get(t.Context(), key)
					if after != rejected {
						t.Fatalf("cached hit concealed the transaction failure: %v", after)
					}

					return nil // A swallowed error must still poison this read transaction.
				})
				if err == nil {
					t.Fatal("poisoned snapshot committed")
				}
			})
		})
	}
}

func TestReadSnapshotMemoEntryCapacityFallsBack(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)

		keys := make([]Key, 70)
		for i := range keys {
			keys[i] = Key{Family: Payload, Parts: [][]byte{[]byte(strconv.Itoa(i))}}
		}

		err := c.View(t.Context(), func(tx *Tx) error {
			count := &readerWorkQuery{base: tx.q}
			tx.q = count

			for range 2 {
				for _, key := range keys {
					_, err := tx.Get(t.Context(), key)
					assertCode(t, err, memory.CodeNotFound)
				}
			}

			if count.reads != 76 {
				t.Fatalf("bounded memo issued %d queries; want64 retained and6 uncached twice", count.reads)
			}

			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestReadSnapshotMemoByteCapacityFallsBack(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		keys := []Key{{Family: Payload, Parts: [][]byte{[]byte("first")}},
			{Family: Payload, Parts: [][]byte{[]byte("second")}}}

		want := envelope(t, Payload, strings.Repeat("x", MaxEnvelopeBytes/2))
		for _, key := range keys {
			mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), key, want) })
		}

		err := c.View(t.Context(), func(tx *Tx) error {
			count := &readerWorkQuery{base: tx.q}
			tx.q = count

			for range 2 {
				for _, key := range keys {
					got, err := tx.Get(t.Context(), key)
					if err != nil {
						return err
					}

					if !bytes.Equal(got, want) {
						t.Fatal("memo byte capacity rejected or changed a valid large value")
					}
				}
			}

			if count.reads != 3 {
				t.Fatalf("byte-bounded memo issued %d queries; want second value uncached", count.reads)
			}

			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestReadSnapshotMemoCountsRetainedKeyBytes(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)

		keys := make([]Key, 64)
		for i := range keys {
			prefix := strconv.Itoa(i)
			keys[i] = Key{Family: Payload, Parts: [][]byte{[]byte(prefix +
				strings.Repeat("x", math.MaxUint16-len(prefix)))}}
		}

		err := c.View(t.Context(), func(tx *Tx) error {
			count := &readerWorkQuery{base: tx.q}
			tx.q = count

			for range 2 {
				for _, key := range keys {
					_, err := tx.Get(t.Context(), key)
					assertCode(t, err, memory.CodeNotFound)
				}
			}

			if count.reads != 65 {
				t.Fatalf("long-key memo issued %d queries; want63 retained and last key uncached", count.reads)
			}

			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestPostgresReadMemoRejectsCorruptFirstPoint(t *testing.T) {
	for _, fault := range []string{"duplicate_row", "raw_binding", "partition_binding", "materialized_binding"} {
		t.Run(fault, func(t *testing.T) {
			db := canonicalsqltest.PostgresDatabase(t)
			c := testConnection(t, db)
			state := memory.ScopeState{SchemaVersion: memory.CanonicalSchemaVersionV1,
				Scope: testScope(), Version: 1, ChangeSeq: 1}
			key := Key{Family: ScopeState, Scope: state.Scope}

			mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), key, envelope(t, ScopeState, state)) })
			corruptScopeFixture(t, c, fault)
			err := c.View(t.Context(), func(tx *Tx) error {
				_, rejected := tx.Get(t.Context(), key)
				assertCode(t, rejected, memory.CodeStoreFailure)

				_, again := tx.Get(t.Context(), key)
				if again != rejected {
					t.Fatal("memo concealed a poisoned physical point read")
				}

				return nil
			})
			assertCode(t, err, memory.CodeStoreFailure)
		})
	}
}

func TestReadSnapshotReusesNormalAbsence(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		key := Key{Family: Payload, Parts: [][]byte{[]byte("absent")}}

		err := c.View(t.Context(), func(tx *Tx) error {
			count := &readerWorkQuery{base: tx.q}
			tx.q = count

			for range 2 {
				_, err := tx.Get(t.Context(), key)
				assertCode(t, err, memory.CodeNotFound)
			}

			if count.reads != 1 {
				t.Fatalf("repeated absent point issued %d physical queries; want one", count.reads)
			}

			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}
