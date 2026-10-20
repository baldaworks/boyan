//go:build integration

package canonicalcore_test

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	m "github.com/baldaworks/boyan/pkg/memory"
)

func coreSQLKey(family canonicalsql.Family, scope m.Scope, parts ...[]byte) canonicalsql.Key {
	return canonicalsql.Key{Family: family, Scope: scope, Parts: parts}
}

func deleteCoreSQLRecord(t *testing.T, owner coreOwner, keys ...canonicalsql.Key) {
	t.Helper()
	err := coreRawConnection(t, owner).Update(t.Context(), func(tx *canonicalsql.Tx) error {
		for _, key := range keys {
			if err := tx.Delete(t.Context(), key); err != nil {
				return err
			}
		}

		return nil
	})
	requireNoError(t, err, "delete SQL fixture records: %v")
}

func writeCoreSQLRecord(t *testing.T, owner coreOwner, key canonicalsql.Key, value any) {
	t.Helper()

	data, err := canonicalsql.Encode(key.Family, value)
	requireNoError(t, err, "encode SQL fixture: %v")
	err = coreRawConnection(t, owner).Update(t.Context(), func(tx *canonicalsql.Tx) error {
		return tx.Put(t.Context(), key, data)
	})
	requireNoError(t, err, "write SQL fixture: %v")
}

// Unlike ordinary writes this deliberately bypasses envelope validation to
// preserve hostile persisted fixtures; identifiers come from literal tests.
func alterCoreSQLEnvelope(t *testing.T, owner coreOwner, table, predicate string, data []byte, args ...any) {
	t.Helper()

	value, ok := coreDatabases.Load(owner)
	if !ok {
		t.Fatalf("missing SQL fixture for %T", owner)
	}

	database := value.(*canonicalsqltest.Database)

	if strings.ContainsAny(table, " ;\"'") {
		t.Fatal("invalid fixture table")
	}

	args = append([]any{data}, args...)
	if database.Postgres != nil {
		_, err := database.Postgres.Exec(
			t.Context(),
			"UPDATE boyan_canonical."+table+" SET envelope=$1 WHERE "+predicate,
			args...)
		requireNoError(t, err, "alter SQL fixture envelope: %v")

		return
	}

	db, err := sql.Open("sqlite", database.DSN)

	requireNoError(t, err, "open raw envelope fixture: %v")
	defer func() { requireNoError(t, db.Close(), "close raw envelope fixture: %v") }()

	_, err = db.ExecContext(t.Context(), "UPDATE "+table+" SET envelope=$1 WHERE "+predicate, args...)
	requireNoError(t, err, "alter SQL fixture envelope: %v")
}

// Raw access is confined to fixtures, while all operations under assertion use
// the real public owner. Callers quiesce it before altering persisted state.
func coreRawConnection(t *testing.T, owner coreOwner) *canonicalsql.Connection {
	t.Helper()

	value, ok := coreDatabases.Load(owner)
	if !ok {
		t.Fatalf("missing SQL fixture for %T", owner)
	}

	database := value.(*canonicalsqltest.Database)
	if database.Postgres != nil {
		return canonicalsql.NewPostgres(database.Postgres)
	}

	db, err := sql.Open("sqlite", database.DSN)
	requireNoError(t, err, "raw SQL fixture: %v")
	t.Cleanup(func() { requireNoError(t, db.Close(), "close raw database: %v") })
	conn, err := db.Conn(t.Context())
	requireNoError(t, err, "raw SQL connection: %v")
	t.Cleanup(func() { requireNoError(t, conn.Close(), "close raw connection: %v") })

	return canonicalsql.NewSQLite(conn)
}

func readCoreRecord(t *testing.T, owner coreOwner, key canonicalsql.Key, target any) {
	t.Helper()
	err := coreRawConnection(t, owner).View(t.Context(), func(tx *canonicalsql.Tx) error {
		data, err := tx.Get(t.Context(), key)
		if err != nil {
			return err
		}

		return canonicalsql.Decode(key.Family, data, target)
	})
	requireNoError(t, err, "read persisted SQL fixture: %v")
}

func coreSQLPayloadRefs(t *testing.T, owner coreOwner) []m.PayloadRef {
	t.Helper()

	var refs []m.PayloadRef

	err := coreRawConnection(t, owner).View(t.Context(), func(tx *canonicalsql.Tx) error {
		for _, family := range []canonicalsql.Family{canonicalsql.Source, canonicalsql.Message, canonicalsql.Revision} {
			var after *canonicalsql.Key
			for {
				rows, err := tx.Scan(t.Context(), canonicalsql.Range{Family: family, After: after, Limit: 32})
				if err != nil {
					return err
				}

				for _, row := range rows {
					envelope, err := m.UnmarshalRecordEnvelope(row.Envelope)
					if err != nil {
						return err
					}

					var record struct {
						Payload m.PayloadRef `json:"payload"`
					}
					if err := json.Unmarshal(envelope.Payload, &record); err != nil {
						return err
					}

					refs = append(refs, record.Payload)
				}

				if len(rows) < 32 {
					break
				}

				after = &rows[len(rows)-1].Key
			}
		}

		return nil
	})
	requireNoError(t, err, "enumerate SQL payload references: %v")

	if len(refs) == 0 {
		t.Fatal("fixture has no payload refs")
	}

	return refs
}
