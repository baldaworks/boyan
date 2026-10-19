package boyan

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCanonicalDefaultsAndForwarding(t *testing.T) {
	config := Config{StateDir: filepath.Join(t.TempDir(), "untouched")}

	normalized, err := config.normalized()
	if err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(config.StateDir, "canonical", "canonical.sqlite")
	if normalized.Canonical.Backend != "sqlite" || normalized.Canonical.SQLite.Path != want {
		t.Fatalf("unexpected canonical defaults: %+v", normalized.Canonical)
	}

	runtime := standaloneRuntimeConfig(normalized)
	if runtime.Canonical.Backend != "sqlite" || runtime.Canonical.SQLite.Path != want {
		t.Fatal("canonical selection was not forwarded")
	}

	if _, err := os.Stat(config.StateDir); !os.IsNotExist(err) {
		t.Fatal("configuration created storage")
	}

	config.Canonical.Backend = " SQLite "
	config.Canonical.SQLite.Path = filepath.Join(t.TempDir(), "explicit.sqlite")

	normalized, err = config.normalized()
	if err != nil || normalized.Canonical.SQLite.Path != config.Canonical.SQLite.Path {
		t.Fatal("explicit path lost")
	}
}

func TestCanonicalPostgresSecretBoundary(t *testing.T) {
	const secret = "private-canonical-password"
	t.Setenv("BOYAN_POSTGRES_DSN", "host=127.0.0.1 password="+secret+" sslmode=disable")

	config := DefaultConfig()
	config.Canonical.Backend = " Postgres "
	config.Canonical.Postgres.AllowLocalInsecure = true

	normalized, err := config.normalized()
	if err != nil {
		t.Fatal(err)
	}

	resolved, err := normalized.resolveCanonical(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if resolved.Postgres.DSN == "" || resolved.Postgres.ConnectTimeout != 5*time.Second {
		t.Fatal("secret selection or timeout lost")
	}

	t.Setenv("BOYAN_POSTGRES_DSN", "changed")

	if !strings.Contains(resolved.Postgres.DSN, secret) {
		t.Fatal("resolved secret was not frozen")
	}

	for _, output := range []string{
		fmt.Sprintf("%v %+v %#v", normalized, normalized, normalized),
		fmt.Sprintf("%v %+v %#v", resolved, resolved, resolved),
	} {
		if strings.Contains(output, secret) {
			t.Fatal("resolved DSN leaked")
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := New(ctx, config); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled boundary lost context")
	}
}

func TestCanonicalInvalidSelectionAndSecret(t *testing.T) {
	for _, tc := range []struct {
		name, backend, path, env, dsn string
		timeout                       time.Duration
		local                         bool
	}{
		{name: "backend", backend: "badger"},
		{name: "sqlite_conflict", backend: "sqlite", env: "OTHER_DSN"},
		{name: "postgres_conflict", backend: "postgres", path: "explicit.sqlite"},
		{name: "missing", backend: "postgres"},
		{name: "invalid_env", backend: "postgres", env: "bad=name"},
		{name: "invalid_secret", backend: "postgres", dsn: "host='private-canonical-password"},
		{name: "remote_insecure", backend: "postgres", dsn: "host=database.example sslmode=disable", local: true},
		{name: "local_consent", backend: "postgres", dsn: "host=127.0.0.1 sslmode=disable"},
		{name: "downgrade", backend: "postgres", dsn: "host=127.0.0.1 sslmode=prefer", local: true},
		{name: "multiple", backend: "postgres", dsn: "host=127.0.0.1,localhost sslmode=disable", local: true},
		{name: "tls_identity", backend: "postgres", dsn: "host=database.example sslmode=require"},
		{name: "negative_timeout", backend: "postgres", dsn: "host=127.0.0.1 sslmode=disable",
			local: true, timeout: -time.Second},
		{name: "large_timeout", backend: "postgres", dsn: "host=127.0.0.1 sslmode=disable",
			local: true, timeout: 31 * time.Second},
		{name: "uri", backend: "sqlite", path: "file:private?mode=memory"},
		{name: "memory", backend: "sqlite", path: ":memory:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BOYAN_POSTGRES_DSN", tc.dsn)

			config := DefaultConfig()
			config.Canonical.Backend = tc.backend
			config.Canonical.SQLite.Path = tc.path
			config.Canonical.Postgres.AllowLocalInsecure = tc.local
			config.Canonical.Postgres.ConnectTimeout = tc.timeout

			if tc.env != "" {
				config.Canonical.Postgres.DSNEnv = tc.env
			}

			err := config.Validate()
			if err == nil {
				t.Fatal("unsafe configuration accepted")
			}

			if strings.Contains(err.Error(), "private-canonical-password") {
				t.Fatal("secret leaked in error")
			}
		})
	}
}

//nolint:cyclop // The two real filesystem fixtures cover independent alias mechanisms without scanning old data.
func TestCanonicalPathAliasesFailWithoutMutation(t *testing.T) {
	root := t.TempDir()
	for _, reserved := range []string{"search", "badger"} {
		path := filepath.Join(root, reserved)
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}

		filename := "search.sqlite"
		if reserved == "badger" {
			filename = "MANIFEST"
		}

		original := filepath.Join(path, filename)
		if err := os.WriteFile(original, []byte("old state"), 0600); err != nil {
			t.Fatal(err)
		}

		alias := filepath.Join(root, reserved+"-alias")
		if err := os.Symlink(path, alias); err != nil {
			t.Fatal(err)
		}

		linked := filepath.Join(root, reserved+"-hardlink.sqlite")
		if err := os.Link(original, linked); err != nil {
			t.Fatal(err)
		}

		for _, candidate := range []string{
			path, filepath.Join(path, "new.sqlite"), filepath.Join(alias, "missing", "new.sqlite"), linked, root,
		} {
			config := DefaultConfig()
			config.StateDir = root
			config.Canonical.SQLite.Path = candidate

			if err := config.Validate(); err == nil {
				t.Errorf("reserved alias accepted: %s", candidate)
			}
		}

		data, err := os.ReadFile(original)
		if err != nil || string(data) != "old state" {
			t.Fatal("old state changed")
		}
	}
}

func TestCanonicalDanglingSymlinkAlias(t *testing.T) {
	root := t.TempDir()
	alias := filepath.Join(root, "alias.sqlite")

	if err := os.Symlink(filepath.Join(root, "search", "not-created.sqlite"), alias); err != nil {
		t.Fatal(err)
	}

	config := DefaultConfig()
	config.StateDir = root
	config.Canonical.SQLite.Path = alias

	if err := config.Validate(); err == nil {
		t.Fatal("dangling symlink into disposable search accepted")
	}

	rootAlias := t.TempDir()
	if err := os.Symlink(string(filepath.Separator), filepath.Join(rootAlias, "search")); err != nil {
		t.Fatal(err)
	}

	config.StateDir = rootAlias

	config.Canonical.SQLite.Path = ""
	if err := config.Validate(); err == nil {
		t.Fatal("search ancestor alias at filesystem root accepted")
	}
}

func TestCanonicalAcceptedPostgresPoliciesAndNamedSecret(t *testing.T) {
	for _, dsn := range []string{
		"host=database.example sslmode=verify-full",
		"host=/tmp sslmode=disable", "host=::1 sslmode=disable", "host=localhost sslmode=disable",
	} {
		t.Setenv("CANONICAL_NAMED_DSN", dsn)

		config := DefaultConfig()
		config.Canonical.Backend = "postgres"
		config.Canonical.Postgres.DSNEnv = " CANONICAL_NAMED_DSN "
		config.Canonical.Postgres.ConnectTimeout = time.Nanosecond
		config.Canonical.Postgres.AllowLocalInsecure = true

		normalized, err := config.normalized()
		if err != nil {
			t.Fatal(err)
		}

		resolved, err := normalized.resolveCanonical(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		if resolved.Postgres.DSN != dsn || resolved.Postgres.ConnectTimeout != time.Nanosecond {
			t.Fatal("named secret or accepted timeout changed")
		}
	}
}

func TestCanonicalSQLiteTimeoutBounds(t *testing.T) {
	for _, timeout := range []time.Duration{-time.Second, time.Nanosecond, 6 * time.Second} {
		config := DefaultConfig()
		config.Canonical.SQLite.BusyTimeout = timeout

		if err := config.Validate(); err == nil {
			t.Fatal("invalid SQLite timeout accepted")
		}
	}
}
