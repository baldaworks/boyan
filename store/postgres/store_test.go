package postgres_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/store/postgres"
)

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
