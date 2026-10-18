package postgres_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
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

	if err := reader.RestorePreflight(t.Context()); err != nil {
		t.Fatal(err)
	}

	_, err = db.Postgres.Exec(t.Context(), "INSERT INTO boyan_canonical.restore_control VALUES(1,$1)", []byte("loading"))
	if err != nil {
		t.Fatal(err)
	}

	for _, check := range []func() error{
		func() error { return store.RestorePreflight(t.Context()) },
		func() error { return reader.RestorePreflight(t.Context()) },
	} {
		code, _, _ := memory.ClassifyError(check())
		if code != memory.CodeUnavailable {
			t.Fatal("reader or store bypassed persisted loading fence")
		}
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	if err := reader.RestorePreflight(t.Context()); err == nil {
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

	if err := (*postgres.Reader)(nil).RestorePreflight(t.Context()); err == nil {
		t.Fatal("nil reader ready")
	}
}
