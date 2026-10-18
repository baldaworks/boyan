package canonicalsql

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestPostgresOwnerTLSPolicy(t *testing.T) {
	cases := []struct {
		name  string
		dsn   string
		local bool
		valid bool
	}{
		{"verified_remote", "host=database.example sslmode=verify-full", false, true},
		{"remote_disable", "host=database.example sslmode=disable", true, false},
		{"local_requires_opt_in", "host=127.0.0.1 sslmode=disable", false, false},
		{"loopback_disable", "host=127.0.0.1 sslmode=disable", true, true},
		{"ipv6_loopback", "host=::1 sslmode=disable", true, true},
		{"unix_disable", "host=/tmp sslmode=disable", true, true},
		{"localhost_disable", "host=localhost sslmode=disable", true, true},
		{"prefer_downgrade", "host=localhost sslmode=prefer", true, false},
		{"allow_downgrade", "host=localhost sslmode=allow", true, false},
		{"require_without_identity", "host=database.example sslmode=require", false, false},
		{"verify_ca_without_identity", "host=database.example sslmode=verify-ca", false, false},
		{"second_host", "host=127.0.0.1,database.example sslmode=disable", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := PostgresConfig{DSN: tc.dsn, AllowLocalInsecure: tc.local}

			native, err := config.parse()
			if (err == nil) != tc.valid {
				t.Fatalf("TLS policy accepted=%v, want %v", err == nil, tc.valid)
			}

			if err == nil && native.TLSConfig != nil &&
				(native.TLSConfig.ServerName != "database.example" || native.TLSConfig.InsecureSkipVerify) {
				t.Fatal("verified identity was lost")
			}
		})
	}
}

func TestPostgresOwnerConfigAndErrorsArePrivate(t *testing.T) {
	secret := "private-test-credential"
	for _, config := range []PostgresConfig{
		{}, {DSN: "host='" + secret},
		{DSN: "host=127.0.0.1 password=" + secret + " sslmode=disable"},
		{DSN: "host=127.0.0.1 sslmode=verify-full sslrootcert=/missing/" + secret},
		{DSN: "host=127.0.0.1 sslmode=disable", AllowLocalInsecure: true, ConnectTimeout: -time.Second},
		{DSN: "host=127.0.0.1 sslmode=disable", AllowLocalInsecure: true, ConnectTimeout: 31 * time.Second},
	} {
		owner, err := OpenPostgresOwner(t.Context(), config)
		if owner != nil || err == nil {
			t.Fatal("invalid config admitted")
		}

		for _, output := range []string{err.Error(), fmt.Sprintf("%v %+v %#v", config, config, config)} {
			if strings.Contains(output, secret) {
				t.Fatal("private configuration leaked")
			}
		}
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := OpenPostgresOwner(ctx, PostgresConfig{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled open lost identity: %v", err)
	}
}

func TestPostgresOwnerPermissionAndMigrationCleanup(t *testing.T) {
	db := canonicalsqltest.PostgresDatabase(t)
	config := PostgresConfig{DSN: db.DSN, AllowLocalInsecure: true}
	// PostgreSQL's built-in read-only role cannot migrate. SET ROLE is scoped
	// only to this new fixture connection; no role or operator object is changed.
	denied := config

	denied.DSN += "&options=-c%20role%3Dpg_read_all_data"
	if owner, err := OpenPostgresOwner(t.Context(), denied); owner != nil || err == nil {
		t.Fatal("read-only role migrated")
	}

	owner := openPostgresTestOwner(t, config)
	rawExec(t, owner.connection, "UPDATE boyan_canonical.schema_migration SET checksum='unsupported'")
	rawExec(t, owner.connection, "INSERT INTO boyan_canonical.restore_control VALUES(1,$1)", []byte("loading"))

	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		failed, err := OpenPostgresOwner(t.Context(), config)
		if failed != nil {
			_ = failed.Close()

			t.Fatal("unsupported schema opened")
		}

		assertCode(t, err, memory.CodeInvalidDerived)
	}

	assertFailedPostgresOpenPreservedState(t, db)
}

func assertFailedPostgresOpenPreservedState(t *testing.T, db *canonicalsqltest.Database) {
	t.Helper()

	var (
		checksum string
		marker   []byte
	)

	err := db.Postgres.QueryRow(t.Context(), "SELECT checksum FROM boyan_canonical.schema_migration").Scan(&checksum)
	if err != nil {
		t.Fatal(err)
	}

	err = db.Postgres.QueryRow(t.Context(), "SELECT control FROM boyan_canonical.restore_control").Scan(&marker)
	if err != nil {
		t.Fatal(err)
	}

	if checksum != "unsupported" || string(marker) != "loading" {
		t.Fatal("failed migration changed durable state")
	}

	var count int

	err = db.Postgres.QueryRow(t.Context(), "SELECT count(*) FROM pg_catalog.pg_stat_activity "+
		"WHERE datname=current_database() AND pid<>pg_backend_pid()").Scan(&count)
	if err != nil || count != 0 {
		t.Fatalf("partial open leaked a session: count=%d err=%v", count, err)
	}
}

func openPostgresTestOwner(t *testing.T, config PostgresConfig) *Owner {
	t.Helper()

	owner, err := OpenPostgresOwner(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := owner.Close(); err != nil {
			t.Error(err)
		}
	})

	return owner
}

func TestPostgresOwnerFixedSchemaAndSecondOwnerBeforeMigration(t *testing.T) {
	db := canonicalsqltest.PostgresDatabase(t)
	if _, err := db.Postgres.Exec(t.Context(),
		"CREATE TABLE public.restore_control(singleton integer,control bytea)"); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Postgres.Exec(t.Context(),
		"CREATE FUNCTION public.length(bytea) RETURNS integer LANGUAGE sql IMMUTABLE AS 'SELECT 0'"); err != nil {
		t.Fatal(err)
	}

	config := PostgresConfig{DSN: db.DSN + "&search_path=public,pg_catalog", AllowLocalInsecure: true}

	owner := openPostgresTestOwner(t, config)
	if err := owner.RestorePreflight(t.Context()); err != nil {
		t.Fatal(err)
	}

	rawExec(t, owner.connection, "UPDATE boyan_canonical.schema_migration SET checksum='unsupported'")

	second, err := OpenPostgresOwner(t.Context(), config)
	if second != nil {
		_ = second.Close()

		t.Fatal("second owner opened")
	}

	assertCode(t, err, memory.CodeUnavailable)
}

func TestPostgresOwnerConnectFailureRedactionAndTimeout(t *testing.T) {
	db := canonicalsqltest.PostgresDatabase(t)

	u, err := url.Parse(db.DSN)
	if err != nil {
		t.Fatal("parse owned test URL")
	}

	const secret = "private-owner-test-credential"

	u.User = url.UserPassword(secret, secret)
	config := PostgresConfig{DSN: u.String(), AllowLocalInsecure: true}

	owner, err := OpenPostgresOwner(t.Context(), config)
	if owner != nil || err == nil || memory.IsRetryable(err) ||
		strings.Contains(err.Error(), secret) || errors.Unwrap(err) != nil {
		t.Fatal("native connection failure was acknowledged or leaked private diagnostics")
	}

	config.DSN = db.DSN
	config.ConnectTimeout = time.Nanosecond

	owner, err = OpenPostgresOwner(t.Context(), config)
	if owner != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("bounded connection timeout lost context identity: %v", err)
	}

	config.ConnectTimeout = 0
	openPostgresTestOwner(t, config)
}
