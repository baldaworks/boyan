//go:build integration

// Package sqlitegate_test holds the dependency feasibility gate only.
// It is deliberately disconnected from runtime composition and owns no production schema.
package sqlitegate_test

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
	_ "modernc.org/sqlite/vec"
)

// A missing static vec registration must fail before any backend replacement.
func TestStaticVectorExtension(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})

	var version string
	if err := db.QueryRowContext(t.Context(), "SELECT vec_version()").Scan(&version); err != nil {
		t.Fatal(err)
	}

	if version != "v0.1.9" {
		t.Fatalf("vec_version=%q, want v0.1.9", version)
	}
}
