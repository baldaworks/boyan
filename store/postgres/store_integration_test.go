//go:build integration

package postgres_test

import (
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	"github.com/baldaworks/boyan/store/postgres"
)

func TestStoreReaderShareLifecycleAndFence(t *testing.T) {
	db := canonicalsqltest.PostgresDatabase(t)

	store, err := postgres.Open(t.Context(), postgres.Config{DSN: db.DSN, AllowLocalInsecure: true})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	reader := postgres.NewCanonicalReader(store)

	for _, check := range []func() error{
		func() error { return store.CheckIntegrity(t.Context()) },
		func() error { return reader.CheckIntegrity(t.Context()) },
	} {
		if err := check(); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	if err := reader.CheckIntegrity(t.Context()); err == nil {
		t.Fatal("closed reader retained readiness")
	}
}
