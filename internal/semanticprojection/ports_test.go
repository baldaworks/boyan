//go:build integration

//nolint:testpackage // Exercise consumer ports with the existing real SQLite fixtures.
package semanticprojection

import (
	"context"
	"testing"

	sqlitestore "github.com/baldaworks/boyan/index/sqlite"
)

var _ CandidateLifecycle = (*sqlitestore.Store)(nil)

func TestPortableBuildStorePreservesTypedNilRejection(t *testing.T) {
	t.Parallel()

	f := newSQLiteManagerFixture(t)
	dependencies := f.dependencies()

	control, err := New(dependencies)
	if err != nil {
		t.Fatalf("valid portable dependencies rejected: %v", err)
	}

	if err := control.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	var store *sqlitestore.Store

	dependencies.Store = store

	manager, err := New(dependencies)
	if err == nil {
		_ = manager.Close(context.Background())

		t.Fatal("New accepted typed nil SQLite store through BuildStore")
	}
}
