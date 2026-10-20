package postgres_test

import (
	"fmt"
	"strings"
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

func TestPublicConfigRedactsSecrets(t *testing.T) {
	config := postgres.Config{DSN: "private-connection-secret"}
	if strings.Contains(fmt.Sprintf("%v %+v %#v", config, config, config), config.DSN) {
		t.Fatal("public config exposed connection secret")
	}

	store, err := postgres.Open(t.Context(), config)
	if store != nil || err == nil || strings.Contains(err.Error(), config.DSN) {
		t.Fatal("bad connection was admitted or disclosed")
	}

	if err := (*postgres.Store)(nil).Close(); err != nil {
		t.Fatal(err)
	}

	if err := (*postgres.Reader)(nil).CheckIntegrity(t.Context()); err == nil {
		t.Fatal("nil reader ready")
	}
}
