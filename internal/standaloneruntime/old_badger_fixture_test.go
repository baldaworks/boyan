package standaloneruntime //nolint:testpackage // Static old bytes prove SQL startup never migrates or mutates them.

import (
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Produced by the retired owner from synthetic old:identity/old-identity data,
// checked by its actual reader before a clean close. Tests never open Badger.
func seedPopulatedOldBadger(t *testing.T, root string) {
	t.Helper()

	file, err := os.Open("testdata/populated-old-badger.json.gz.b64")
	requireNoError(t, err, "old fixture bytes: %v")

	defer file.Close()

	compressed, err := gzip.NewReader(base64.NewDecoder(base64.StdEncoding, file))
	requireNoError(t, err, "old fixture compression: %v")

	defer compressed.Close()

	var files map[string][]byte
	requireNoError(t, json.NewDecoder(compressed).Decode(&files), "old fixture files: %v")
	requireNoError(t, os.MkdirAll(root, 0o700), "old fixture directory: %v")

	for name, data := range files {
		if filepath.Base(name) != name || name == "." {
			t.Fatalf("invalid old fixture filename %q", name)
		}

		requireNoError(t, os.WriteFile(filepath.Join(root, name), data, 0o600), "old fixture file: %v")
	}
}
