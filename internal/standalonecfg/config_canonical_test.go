package standalonecfg_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/standalonecfg"
)

func TestCanonicalYAMLAndEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")

	data := "canonical:\n  backend: sqlite\n  sqlite:\n    path: /explicit/canonical.sqlite\n    busy_timeout: 2s\n" +
		"maintenance:\n  value_log_gc_interval: 6h\n  discard_ratio: 0.5\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	config, err := standalonecfg.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	if config.Canonical.SQLite.Path != "/explicit/canonical.sqlite" ||
		config.Canonical.SQLite.BusyTimeout != 2*time.Second {
		t.Fatal("SQLite YAML selection lost")
	}

	t.Setenv("BOYAN_CANONICAL_BACKEND", "postgres")
	t.Setenv("BOYAN_CANONICAL_SQLITE_PATH", "")
	t.Setenv("BOYAN_CANONICAL_POSTGRES_DSN_ENV", "CUSTOM_DSN")
	t.Setenv("BOYAN_CANONICAL_POSTGRES_CONNECT_TIMEOUT", "3s")
	t.Setenv("BOYAN_CANONICAL_POSTGRES_ALLOW_LOCAL_INSECURE", "true")

	config, err = standalonecfg.LoadConfig("")
	if err != nil {
		t.Fatal(err)
	}

	if config.Canonical.Backend != "postgres" || config.Canonical.Postgres.DSNEnv != "CUSTOM_DSN" ||
		config.Canonical.Postgres.ConnectTimeout != 3*time.Second ||
		!config.Canonical.Postgres.AllowLocalInsecure {
		t.Fatal("PostgreSQL environment selection lost")
	}
}

func TestCanonicalUnknownAndInvalidTypes(t *testing.T) {
	for _, data := range []string{
		"canonical:\n  postgres:\n    dsn: private\n", "canonical:\n  sqltie: {}\n",
		"canonical:\n  postgres:\n    connect_timeout: invalid\n",
		"canonical:\n  sqlite:\n    busy_timeout: invalid\n",
	} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}

		if _, err := standalonecfg.LoadConfig(path); err == nil {
			t.Fatal("invalid canonical config decoded")
		}
	}
}
