//go:build integration

package sqlitegate_test

import (
	"context"
	"database/sql"
	"encoding/binary"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// The gate deliberately precreates a private DB. Production secure path policy
// and pool lifecycle belong to task 4, not to this throwaway fixture.
func openFixture(t *testing.T) (*sql.DB, *sql.DB, string) {
	t.Helper()

	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "gate ?#&.sqlite")

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	writer := fixturePool(t, path, false, 1)
	reader := fixturePool(t, path, true, 2)

	return writer, reader, path
}

func fixturePool(t *testing.T, path string, readOnly bool, size int) *sql.DB {
	t.Helper()

	u := url.URL{Scheme: "file", Path: path}

	q := url.Values{}
	for _, pragma := range []string{
		"journal_mode(WAL)", "synchronous(FULL)", "foreign_keys(ON)", "busy_timeout(1000)", "temp_store(MEMORY)",
	} {
		q.Add("_pragma", pragma)
	}

	if readOnly {
		q.Add("_pragma", "query_only(ON)")
	}

	u.RawQuery = q.Encode()

	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		t.Fatal(err)
	}

	db.SetMaxOpenConns(size)
	db.SetMaxIdleConns(size)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})

	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}

	return db
}

func execFixture(t *testing.T, db interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}, statement string, args ...any) {
	t.Helper()

	if _, err := db.ExecContext(t.Context(), statement, args...); err != nil {
		t.Fatal(err)
	}
}

func blob(values ...float32) []byte {
	result := make([]byte, 4*len(values))
	for i, value := range values {
		binary.LittleEndian.PutUint32(result[i*4:], math.Float32bits(value))
	}

	return result
}
