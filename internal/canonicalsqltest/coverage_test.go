package canonicalsqltest //nolint:testpackage // Tests private inventory routing and explicit regeneration.

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"reflect"
	"sort"
	"testing"
	"testing/fstest"
)

var writeCoverage = flag.Bool("write-coverage", false, "regenerate the reviewed preservation inventory")

// Adding an unregistered family, key, write site or consumer case must be visible.
//nolint:funlen // Keep the native key/write/raw-call positive and atomic-read negative controls in one discovery probe.
func TestDiscoverCoverageFindsPrivateAndConsumerSurfaces(t *testing.T) {
	files := fstest.MapFS{
		"internal/canonicalsql/extra.go": {Data: []byte(`package canonicalsql
const Hidden Family = "source-privacy-ready"
func hiddenMarker() []byte { return memory.TupleKey(83, 1, badgerRecordHidden) }
func erase(tx *Tx) { tx.Delete(nil) }
func writeFence() { os.Rename("temporary", "durable") }
func readFlag() { ready.Load() }
func privacyKey() Key { return key }
func eraseOwnedPrefix() Range { return scan }
func readInlineKey() { tx.Get(ctx, Key{Family: Hidden}) }
func writeRow() { tx.Put(ctx, key, value) }
func compareScope() { tx.CompareAndSwapScope(ctx, key, old, next) }
func pgPut() { tx.q.exec(ctx, statement, args) }
func pgDelete() { tx.q.exec(ctx, statement, args) }
func primitivePut() { tx.pgPut(ctx, key, value) }
func primitiveDelete() { tx.pgDelete(ctx, key) }
`)},
		"internal/consumer/consumer_test.go": {Data: []byte(`package consumer
import canonical "github.com/baldaworks/boyan/internal/canonicalsql"
func TestInterleaving(t *testing.T) { store.beforeCommit = func() {}; store.db.Update(nil) }
func corruptFixture() { txn.Set(nil, nil) }
func corruptRawOwner() { owner.connection.q.exec(ctx, sql, args) }
func corruptRawPG() { database.Postgres.Exec(ctx, sql, args) }
func inspectRawPG() { database.Postgres.QueryRow(ctx, sql, args) }
func corruptRawSQLite() { conn.ExecContext(ctx, sql, args) }
func inspectRawSQLite() { conn.QueryContext(ctx, sql, args) }
`)},
	}

	entries, err := DiscoverCoverage(files)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]bool{
		"family:Hidden":                false,
		"key:hiddenMarker":             false,
		"writer:erase":                 false,
		"writer:writeFence":            false,
		"key:privacyKey":               false,
		"key:eraseOwnedPrefix":         false,
		"key:readInlineKey":            false,
		"writer:writeRow":              false,
		"writer:compareScope":          false,
		"writer:pgPut":                 false,
		"writer:pgDelete":              false,
		"writer:primitivePut":          false,
		"writer:primitiveDelete":       false,
		"case:TestInterleaving":        false,
		"raw-fixture:TestInterleaving": false,
		"fixture:corruptFixture":       false,
		"raw-fixture:corruptFixture":   false,
		"raw-fixture:corruptRawOwner":  false,
		"raw-fixture:corruptRawPG":     false,
		"raw-fixture:inspectRawPG":     false,
		"raw-fixture:corruptRawSQLite": false,
		"raw-fixture:inspectRawSQLite": false,
	}

	for _, entry := range entries {
		if entry.Kind == "writer" && entry.Symbol == "readFlag" {
			t.Error("atomic read was inventoried as a durable writer")
		}

		key := entry.Kind + ":" + entry.Symbol
		if _, exists := want[key]; exists {
			want[key] = true
		}

		if entry.Owner == "" {
			t.Errorf("missing migration owner: %s", key)
		}
	}

	for key, found := range want {
		if !found {
			t.Errorf("unregistered preservation surface: %s", key)
		}
	}
}

//nolint:cyclop,funlen // Explicit registration diagnostics and opt-in manifest regeneration.
func TestPreservationInventory(t *testing.T) {
	entries, err := DiscoverCoverage(os.DirFS("../.."))
	if err != nil {
		t.Fatal(err)
	}

	if *writeCoverage {
		var data bytes.Buffer
		data.WriteString("[\n")

		for i, entry := range entries {
			encoded, err := json.Marshal(entry)
			if err != nil {
				t.Fatal(err)
			}

			data.Write(encoded)

			if i+1 < len(entries) {
				data.WriteByte(',')
			}

			data.WriteByte('\n')
		}

		data.WriteString("]\n")

		if err := os.WriteFile("coverage.json", data.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}

		return
	}

	registered, err := RegisteredCoverage()
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(entries, registered) {
		actual := make(map[string]bool)

		for _, entry := range entries {
			data, _ := json.Marshal(entry)
			actual[string(data)] = true
		}

		var changed []string

		for _, entry := range registered {
			data, _ := json.Marshal(entry)
			if !actual[string(data)] {
				changed = append(changed, "changed or removed registration: "+string(data))
			}

			delete(actual, string(data))
		}

		for entry := range actual {
			changed = append(changed, "unregistered preservation surface: "+entry)
		}

		sort.Strings(changed)

		for i, entry := range changed {
			if i == 12 {
				break
			}

			t.Log(entry)
		}

		t.Logf("%d preservation registrations changed; showing at most 12", len(changed))
		t.Fatal("review owners and preserve coverage before regenerating with -write-coverage")
	}

	t.Logf("verified %d registered key/family/writer/fixture/case surfaces", len(entries))
}
