package canonicalsql

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"strings"
)

//go:embed 001_sqlite.sql
var sqliteDDL string

//go:embed 001_postgres.sql
var postgresDDL string

// Migrate applies the embedded initial additive schema transactionally. The
// owner must hold exclusive store ownership. Unknown history is never repaired.
func (c *Connection) Migrate(ctx context.Context) error {
	return c.run(ctx, true, func(tx *Tx) error {
		ddl := sqliteDDL
		if c.pg != nil {
			ddl = postgresDDL

			if _, err := tx.q.exec(ctx, "CREATE SCHEMA IF NOT EXISTS boyan_canonical"); err != nil {
				return databaseError(ctx, "create schema", err)
			}
		}

		_, err := tx.q.exec(ctx, "CREATE TABLE IF NOT EXISTS "+c.table("schema_migration")+
			" (version INTEGER PRIMARY KEY CHECK (version>0), checksum TEXT NOT NULL)")
		if err != nil {
			return databaseError(ctx, "migration history", err)
		}

		checksum := migrationChecksum(ddl)

		applied, err := c.checkMigration(ctx, tx.q, []string{checksum})
		if err != nil {
			return err
		}

		if applied == 0 {
			if err := tx.initialize(ctx, ddl, checksum); err != nil {
				return err
			}
		}

		return tx.CheckIntegrity(ctx)
	})
}

func migrationChecksum(ddl string) string {
	digest := sha256.Sum256([]byte(ddl))

	return hex.EncodeToString(digest[:])
}

func (c *Connection) checkMigration(ctx context.Context, q query, checksums []string) (int, error) {
	r, err := q.query(
		ctx,
		"SELECT version,checksum FROM "+c.table("schema_migration")+" ORDER BY version",
	)
	if err != nil {
		return 0, databaseError(ctx, "read migration", err)
	}
	defer r.Close()

	applied := 0

	for r.Next() {
		var (
			version int
			stored  string
		)
		if err := r.Scan(&version, &stored); err != nil {
			return 0, databaseError(ctx, "read migration", err)
		}

		if version != applied+1 || version > len(checksums) || stored != checksums[version-1] {
			return 0, invalid("unsupported canonical SQL schema history")
		}

		applied++
	}

	if err := r.Err(); err != nil {
		return 0, databaseError(ctx, "read migration", err)
	}

	if err := r.Close(); err != nil {
		return 0, databaseError(ctx, "close migration rows", err)
	}

	return applied, nil
}

func applyDDL(ctx context.Context, q query, ddl string) error {
	for _, statement := range strings.Split(ddl, ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}

		if _, err := q.exec(ctx, statement); err != nil {
			return databaseError(ctx, "apply migration", err)
		}
	}

	return nil
}

func (tx *Tx) initialize(ctx context.Context, ddl, checksum string) error {
	if err := applyDDL(ctx, tx.q, ddl); err != nil {
		return err
	}

	_, err := tx.q.exec(ctx, "INSERT INTO "+tx.connection.table("schema_migration")+
		" (version,checksum) VALUES(1,$1)", checksum)
	if err != nil {
		return databaseError(ctx, "record migration", err)
	}

	return tx.CheckIntegrity(ctx)
}
