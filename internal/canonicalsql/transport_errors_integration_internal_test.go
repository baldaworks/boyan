//go:build integration

package canonicalsql

import (
	"errors"
	"net"
	"net/url"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Exercise the actual pgx wrapper chain and owner constructor against a
// private stale Unix socket, never a foreign endpoint or running service.
func TestPostgresOwnerRefusedConnectionRetryable(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, ".s.PGSQL.5432")

	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatal("create private transport fixture")
	}

	listener.SetUnlinkOnClose(false)

	if err := listener.Close(); err != nil {
		t.Fatal("close private transport fixture")
	}

	const secret = "private-transport-test-secret"

	u := url.URL{Scheme: "postgres", User: url.UserPassword(secret, secret), Path: "/" + secret}
	u.RawQuery = url.Values{"host": {dir}, "port": {"5432"}, "sslmode": {"disable"}}.Encode()
	config := PostgresConfig{DSN: u.String(), AllowLocalInsecure: true}

	nativeConfig, err := config.parse()
	if err != nil {
		t.Fatal("parse private fixture configuration")
	}

	conn, nativeErr := pgx.ConnectConfig(t.Context(), nativeConfig)
	if conn != nil {
		_ = conn.Close(t.Context())
		t.Fatal("closed private socket accepted native connection")
	}

	var (
		connectErr *pgconn.ConnectError
		opErr      *net.OpError
	)
	if !errors.As(nativeErr, &connectErr) || !errors.As(nativeErr, &opErr) ||
		!errors.Is(nativeErr, syscall.ECONNREFUSED) {
		t.Fatal("fixture did not produce a pgx-wrapped native refused connection")
	}

	owner, err := OpenPostgresOwner(t.Context(), config)
	if owner != nil {
		_ = owner.Close()

		t.Fatal("unavailable endpoint opened")
	}

	assertPrivateTransportError(t, err, true, secret, dir)
}
