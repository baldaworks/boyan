//go:build integration

package canonicalsqltest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
)

const fixtureDatabasePrefix = "boyan_test_"

func fixtureDatabaseName() (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", errors.New("generate postgres fixture identity")
	}

	return fixtureDatabasePrefix + hex.EncodeToString(nonce[:]), nil
}

func (db *Database) dropOwnedDatabase(ctx context.Context) error {
	suffix := strings.TrimPrefix(db.databaseName, fixtureDatabasePrefix)

	decoded, err := hex.DecodeString(suffix)
	if err != nil || len(decoded) != 16 || db.databaseName == suffix || db.databaseOID == "" || db.ownerOID == "" {
		return errors.New("refuse postgres fixture cleanup: unverified database identity")
	}

	var oid, owner string

	identity := db.admin.QueryRow(ctx, "SELECT oid::text, datdba::text FROM pg_database WHERE datname=$1", db.databaseName)
	if err := identity.Scan(&oid, &owner); err != nil {
		return errors.New("verify postgres fixture cleanup identity")
	}

	if oid != db.databaseOID || owner != db.ownerOID {
		return errors.New("refuse postgres fixture cleanup: database identity changed")
	}
	// The exact generated identifier is sanitized; no caller-selected database or
	// schema is dropped. No FORCE: leaked clients make cleanup fail visibly.
	if _, err := db.admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{db.databaseName}.Sanitize()); err != nil {
		return errors.New("drop owned postgres fixture database (check leaked test connections)")
	}

	return nil
}

func fixtureDSN(dsn, name string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, _ := url.Parse(dsn) // The original DSN was validated by pgx first.
		parsed.Path = "/" + name
		parsed.RawPath = ""
		query := parsed.Query()
		query.Del("database")
		query.Set("dbname", name)
		parsed.RawQuery = query.Encode()

		return parsed.String()
	}
	// The name contains only a fixed prefix and hexadecimal nonce. Last keyword
	// wins, including when the original uses pgx's "database" alias.
	return dsn + " dbname=" + name
}
