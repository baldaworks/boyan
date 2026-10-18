package canonicalsql

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func testConnection(t *testing.T, db *canonicalsqltest.Database) *Connection {
	t.Helper()

	var c *Connection
	if db.SQLite != nil {
		c = NewSQLite(db.SQLite)
	} else {
		c = NewPostgres(db.Postgres)
	}

	if err := c.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}

	return c
}

func testScope() memory.Scope {
	return memory.Scope{Key: "chat:a\x00:b", Kind: memory.ScopeKindPersonal}
}

func envelope(t *testing.T, f Family, value any) []byte {
	t.Helper()

	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}

	encoded, err := memory.MarshalRecordEnvelope(string(f), payload)
	if err != nil {
		t.Fatal(err)
	}

	return encoded
}

// Catches omitted families, accidental synthetic scopes, and missing durable tables.
func TestEveryInventoriedFamilyRoundtrip(t *testing.T) {
	data, err := os.ReadFile("../canonicalsqltest/coverage.json")
	if err != nil {
		t.Fatal(err)
	}

	var entries []canonicalsqltest.CoverageEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatal(err)
	}

	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)

		for _, e := range entries {
			if e.Kind != "family" {
				continue
			}

			t.Run(e.Detail, func(t *testing.T) { testFamilyRoundtrip(t, c, Family(e.Detail)) })
		}

		if err := c.Migrate(t.Context()); err != nil {
			t.Fatalf("idempotent migration: %v", err)
		}

		assertClock(t, c, 38, 38)
	})
}

func testFamilyRoundtrip(t *testing.T, c *Connection, f Family) {
	t.Helper()
	key := familyFixtureKey(t, f)

	var value any = true

	switch f {
	case ScopeState:
		value = memory.ScopeState{SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: key.Scope, Version: 1, ChangeSeq: 1}
	case Message:
		value = memory.MessageRecord{MessageID: string(key.Parts[0]), SourceID: "source"}
	case Revision:
		value = memory.MemoryRevision{RevisionID: string(key.Parts[0]), ItemID: "item", Revision: 1}
	}

	encoded := envelope(t, f, value)
	mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), key, encoded) })

	if err := c.View(t.Context(), func(tx *Tx) error {
		got, err := tx.Get(t.Context(), key)
		if err == nil && !bytes.Equal(got, encoded) {
			t.Error("durable envelope changed")
		}

		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func familyFixtureKey(t *testing.T, f Family) Key {
	t.Helper()

	def, err := familyDefinition(f)
	if err != nil {
		t.Fatal(err)
	}

	key := Key{Family: f}
	if def.scoped {
		key.Scope = testScope()
	}

	for _, col := range def.parts {
		part := []byte("a\x00|:b")
		if col == "sequence_u64" {
			part = Uint64(1)
		}

		key.Parts = append(key.Parts, part)
	}

	return key
}
