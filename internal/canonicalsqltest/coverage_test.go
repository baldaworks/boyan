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
func TestDiscoverCoverageFindsPrivateAndConsumerSurfaces(t *testing.T) {
	files := fstest.MapFS{
		"store/badger/extra.go": {Data: []byte(`package badger
import "github.com/dgraph-io/badger/v4"
const badgerRecordHidden = "source-privacy-ready"
func hiddenMarker() []byte { return memory.TupleKey(83, 1, badgerRecordHidden) }
func erase(tx *badger.Txn) { tx.Delete(nil) }
func writeFence() { os.Rename("temporary", "durable") }
func readFlag() { ready.Load() }
`)},
		"internal/consumer/consumer_test.go": {Data: []byte(`package consumer
import canonicalbadger "github.com/baldaworks/boyan/store/badger"
func TestInterleaving(t *testing.T) { store.beforeCommit = func() {}; store.db.Update(nil) }
func corruptFixture() { txn.Set(nil, nil) }
`)},
	}

	entries, err := DiscoverCoverage(files)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]bool{
		"family:badgerRecordHidden":    false,
		"key:hiddenMarker":             false,
		"writer:erase":                 false,
		"writer:writeFence":            false,
		"case:TestInterleaving":        false,
		"raw-fixture:TestInterleaving": false,
		"fixture:corruptFixture":       false,
		"raw-fixture:corruptFixture":   false,
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
