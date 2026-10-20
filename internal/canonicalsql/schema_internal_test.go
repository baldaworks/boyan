package canonicalsql

import (
	"encoding/json"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

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

func familyFixtureKey(t *testing.T, f Family) Key {
	t.Helper()

	var def familyDef

	found := false

	for _, shape := range expectedShapes {
		if shape.family == f {
			def = familyDef{table: shape.table, scoped: shape.scoped, parts: shape.parts}
			found = true

			break
		}
	}

	if !found {
		t.Fatalf("missing independent fixture for %s", f)
	}

	key := Key{Family: f}
	if def.scoped {
		key.Scope = testScope()
	}

	for i, col := range def.parts {
		part := []byte("a\x00|:b" + string(rune('a'+i)))
		if col == "sequence_u64" {
			part = Uint64(1)
		}

		key.Parts = append(key.Parts, part)
	}

	return key
}
