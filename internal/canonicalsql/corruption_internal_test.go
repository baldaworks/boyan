package canonicalsql

import (
	"strings"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestPostgresExactResolverRejectsCorruption(t *testing.T) {
	faults := []string{"duplicate_row", "duplicate_stamp", "raw_binding", "partition_binding", "materialized_binding"}
	for _, fault := range faults {
		t.Run(fault, func(t *testing.T) {
			canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
				if db.Postgres == nil {
					return
				}

				c := testConnection(t, db)
				state := memory.ScopeState{
					SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: testScope(), Version: 1, ChangeSeq: 1,
				}
				key := Key{Family: ScopeState, Scope: state.Scope}
				encoded := envelope(t, ScopeState, state)
				mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), key, encoded) })

				corruptScopeFixture(t, c, fault)

				for _, operation := range []string{"get", "put", "delete", "cas"} {
					t.Run(operation, func(t *testing.T) {
						err := c.Update(t.Context(), func(tx *Tx) error {
							switch operation {
							case "get":
								_, _ = tx.Get(t.Context(), key)
							case "put":
								_ = tx.Put(t.Context(), key, encoded)
							case "delete":
								_ = tx.Delete(t.Context(), key)
							case "cas":
								expected := uint64(1)
								_ = tx.CompareAndSwapScope(t.Context(), &expected, state)
							}

							if tx.sequence != nil {
								t.Error("corruption advanced transaction clock before rejection")
							}

							return nil // A swallowed primitive error must still abort.
						})
						assertCode(t, err, memory.CodeStoreFailure)
					})
				}

				stampCount := 1
				if fault == "duplicate_stamp" {
					stampCount = 2
				}

				assertClock(t, c, 1, stampCount)
				err := c.View(t.Context(), func(tx *Tx) error { return tx.CheckIntegrity(t.Context()) })
				assertCode(t, err, memory.CodeStoreFailure)
				assertCode(t, c.Migrate(t.Context()), memory.CodeStoreFailure)
			})
		})
	}
}

func TestPostgresPrivateSequenceExhaustionRollsBack(t *testing.T) {
	for _, target := range []string{"source.row_id", "row_stamp.stamp_id"} {
		t.Run(target, func(t *testing.T) {
			canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
				if db.Postgres == nil {
					return
				}

				c := testConnection(t, db)
				parts := strings.Split(target, ".")
				rawExec(t, c, "SELECT setval(pg_get_serial_sequence($1,$2),9223372036854775807,true)", c.table(parts[0]), parts[1])

				key := Key{Family: Source, Scope: testScope(), Parts: [][]byte{[]byte("new")}}
				err := c.Update(t.Context(), func(tx *Tx) error {
					_ = tx.Put(t.Context(), key, envelope(t, Source, true))

					return nil
				})
				assertCode(t, err, memory.CodeLimitExceeded)
				assertClock(t, c, 0, 0)

				if err := c.View(t.Context(), func(tx *Tx) error {
					_, err := tx.Get(t.Context(), key)
					assertCode(t, err, memory.CodeNotFound)

					return nil
				}); err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}

func TestIntegrityRejectsMissingOrContradictoryStamps(t *testing.T) {
	for _, fault := range []string{"missing_stamp", "deleted_live", "missing_live", "wrong_frame"} {
		t.Run(fault, func(t *testing.T) {
			canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
				c := testConnection(t, db)
				key := Key{Family: Source, Scope: testScope(), Parts: [][]byte{[]byte("id")}}

				mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), key, envelope(t, Source, true)) })

				switch fault {
				case "missing_stamp":
					rawExec(t, c, "DELETE FROM "+c.table("row_stamp"))
				case "deleted_live":
					rawExec(t, c, "UPDATE "+c.table("row_stamp")+" SET deleted=$1", true)
				case "missing_live":
					rawExec(t, c, "DELETE FROM "+c.table("source"))
				case "wrong_frame":
					rawExec(t, c, "UPDATE "+c.table("row_stamp")+" SET binary_key=$1", []byte{1, 0, 0, 0, 2, 1})
				}

				err := c.View(t.Context(), func(tx *Tx) error { return tx.CheckIntegrity(t.Context()) })
				assertCode(t, err, memory.CodeStoreFailure)
			})
		})
	}
}

func corruptScopeFixture(t *testing.T, c *Connection, fault string) {
	t.Helper()

	switch fault {
	case "duplicate_row":
		rawExec(t, c, "INSERT INTO "+c.table("scope_state")+
			" (logical_key,scope_partition,scope_key,scope_kind,envelope,version_u64,change_seq_u64) "+
			"SELECT logical_key,scope_partition,scope_key,scope_kind,envelope,version_u64,change_seq_u64 FROM "+
			c.table("scope_state"))
	case "duplicate_stamp":
		rawExec(t, c, "INSERT INTO "+c.table("row_stamp")+" (family,binary_key,last_seq,deleted) "+
			"SELECT family,binary_key,last_seq,deleted FROM "+c.table("row_stamp"))
	case "raw_binding":
		rawExec(t, c, "UPDATE "+c.table("scope_state")+" SET scope_key=$1", []byte("chat:foreign"))
	case "partition_binding":
		rawExec(t, c, "UPDATE "+c.table("scope_state")+" SET scope_partition=$1", []byte("corrupt"))
	case "materialized_binding":
		rawExec(t, c, "UPDATE "+c.table("scope_state")+" SET version_u64=$1", Uint64(7))
	}
}
