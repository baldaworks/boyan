//go:build integration

package canonicalsql

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/jackc/pgx/v5/pgconn"
)

// Catches PostgreSQL indexes narrowing the existing 65535-byte component
// contract. Hash-derived printable bytes avoid repeated-character compression
// hiding the native index-entry limit. Both engines must persist the same keys.
func TestLongBinaryKeysPreserveAcceptedDomain(t *testing.T) {
	shortScope := memory.Scope{Key: "chat:long-key-test", Kind: memory.ScopeKindPersonal}
	longScope := memory.Scope{Key: "chat:" + deterministicLongID(16*1024), Kind: memory.ScopeKindPersonal}

	cases := []struct {
		name      string
		key       Key
		statement string
	}{
		{
			name:      "id_16k",
			key:       Key{Family: Source, Scope: shortScope, Parts: [][]byte{[]byte(deterministicLongID(16 * 1024))}},
			statement: "INSERT INTO boyan_canonical.source(scope_key,scope_kind,id,envelope) VALUES($1,$2,$3,$4)",
		},
		{
			name:      "scope_16k",
			key:       Key{Family: Source, Scope: longScope, Parts: [][]byte{[]byte("id")}},
			statement: "INSERT INTO boyan_canonical.source(scope_key,scope_kind,id,envelope) VALUES($1,$2,$3,$4)",
		},
		{
			name: "compound_8k_each",
			key: Key{Family: ProvenanceForward, Scope: shortScope, Parts: [][]byte{
				[]byte(deterministicLongID(8 * 1024)), []byte(deterministicLongID(8 * 1024)),
			}},
			statement: "INSERT INTO boyan_canonical.provenance_forward " +
				"(scope_key,scope_kind,from_id,to_id,envelope) VALUES($1,$2,$3,$4,$5)",
		},
		{
			name:      "id_max_65535",
			key:       Key{Family: Source, Scope: shortScope, Parts: [][]byte{[]byte(deterministicLongID(math.MaxUint16))}},
			statement: "INSERT INTO boyan_canonical.source(scope_key,scope_kind,id,envelope) VALUES($1,$2,$3,$4)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			validateLongKeyContract(t, tc.key)
			canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
				testAcceptedLongKey(t, db, tc.key, tc.statement)
			})
		})
	}
}

func deterministicLongID(size int) string {
	encoded := make([]byte, 0, size+sha256.Size*2)
	for counter := uint64(0); len(encoded) < size; counter++ {
		input := binary.BigEndian.AppendUint64([]byte("boyan-canonical-long-key-regression"), counter)
		digest := sha256.Sum256(input)
		encoded = hex.AppendEncode(encoded, digest[:])
	}

	return string(encoded[:size])
}

func validateLongKeyContract(t *testing.T, key Key) {
	t.Helper()

	if err := key.Scope.Validate(); err != nil {
		t.Fatal(err)
	}

	components := []string{string(key.Family), key.Scope.Key, string(key.Scope.Kind)}
	for _, part := range key.Parts {
		components = append(components, string(part))
	}

	if _, err := memory.TupleKey(0x53, 1, components...); err != nil {
		t.Fatalf("existing tuple contract rejected fixture: %v", err)
	}

	if _, _, err := key.values(); err != nil {
		t.Fatalf("SQL key validation rejected fixture: %v", err)
	}
}

// Replay only the failing INSERT in the same disposable database so production
// error redaction stays intact while the regression records the engine cause.
// The diagnostic transaction always rolls back and does not alter the schema.
func logLongKeyIndexFailure(t *testing.T, db *canonicalsqltest.Database, key Key, statement string, encoded []byte) {
	t.Helper()

	tx, err := db.Postgres.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin diagnostic transaction: %v", databaseError(t.Context(), "diagnostic begin", err))
	}
	defer func() {
		if err := tx.Rollback(t.Context()); err != nil {
			t.Error(databaseError(t.Context(), "diagnostic rollback", err))
		}
	}()

	args := []any{[]byte(key.Scope.Key), []byte(key.Scope.Kind)}
	for _, part := range key.Parts {
		args = append(args, part)
	}

	args = append(args, encoded)
	_, err = tx.Exec(t.Context(), statement, args...)

	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		t.Error("raw family INSERT did not reproduce a PostgreSQL server error")

		return
	}

	if pg.Code != "54000" {
		t.Errorf("raw INSERT returned unexpected SQLSTATE %s", pg.Code)

		return
	}
	// This program-limit diagnostic contains index sizes/names, never input data.
	t.Logf("raw PostgreSQL SQLSTATE=%s: %s", pg.Code, pg.Message)
}

func testAcceptedLongKey(t *testing.T, db *canonicalsqltest.Database, key Key, statement string) {
	t.Helper()
	c := testConnection(t, db)
	encoded := envelope(t, key.Family, true)

	err := c.Update(t.Context(), func(tx *Tx) error { return tx.Put(t.Context(), key, encoded) })
	if err != nil {
		if db.Postgres != nil {
			logLongKeyIndexFailure(t, db, key, statement, encoded)
		}

		t.Fatalf("Put rejected identity accepted by existing tuple and SQL key contracts: %v", err)
	}

	if err := c.View(t.Context(), func(tx *Tx) error {
		got, err := tx.Get(t.Context(), key)
		if err == nil && !bytes.Equal(got, encoded) {
			t.Error("long-key roundtrip changed the envelope")
		}

		return err
	}); err != nil {
		t.Fatalf("Get accepted long key: %v", err)
	}
}
