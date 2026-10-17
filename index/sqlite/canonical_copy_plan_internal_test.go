package sqlite

import (
	"strings"
	"testing"
)

// Regressions here catch either a missing fresh index or failure to upgrade an
// otherwise supported database. Inspect the statement actually used by copy.
//
//nolint:cyclop,gocognit // Fresh and supported-reopen scenarios inspect the actual production query plan.
func TestCanonicalCopyUsesIndexedSeek(t *testing.T) {
	for _, reopen := range []bool{false, true} {
		name := "fresh"
		if reopen {
			name = "supported_reopen"
		}

		t.Run(name, func(t *testing.T) {
			store, dir := openTestStore(t, false)
			seedLexical(t, store, "copy-build", lexicalTestScope,
				[]lexicalFixture{{id: "original", raw: "needle", sources: []string{"original-source"}}})

			if reopen {
				// Model a supported version-1 database created before the seek index.
				if _, err := store.writer.ExecContext(t.Context(), "DROP INDEX IF EXISTS lexical_record_build_rowid"); err != nil {
					t.Fatal(err)
				}

				if err := store.Close(); err != nil {
					t.Fatal(err)
				}

				var err error

				store, err = Open(t.Context(), dir, Options{})
				if err != nil {
					t.Fatal(err)
				}

				t.Cleanup(func() { _ = store.Close() })
			}

			observer := observeReader(t, store, dir)

			inputs, last, err := store.loadLexicalPage(t.Context(), "copy-build", 0)
			if err != nil || len(inputs) != 1 || last == 0 {
				t.Fatalf("copy inputs=%d last=%d err=%v", len(inputs), last, err)
			}

			if inputs[0].revision != "original" || len(inputs[0].sources) != 1 || inputs[0].sources[0] != "original-source" {
				t.Fatal("copy changed persisted revision or membership")
			}

			queries := observer.take()
			if len(queries) != 3 {
				t.Fatalf("one-record copy queries = %d, want 3", len(queries))
			}

			joined := observedQueryPlan(t, store, queries[0])
			if strings.Contains(joined, "TEMP B-TREE") || !strings.Contains(joined, "physical_build_id=? AND rowid>?") {
				t.Fatalf("copy must seek within build without sorting: %s", joined)
			}
		})
	}
}
