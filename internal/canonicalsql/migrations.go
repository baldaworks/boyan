package canonicalsql

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"strings"
)

//go:embed 001_sqlite.sql
var sqliteDDL string

//go:embed 001_postgres.sql
var postgresDDL string

//go:embed 002_sqlite.sql
var sqliteRestoreDDL string

//go:embed 002_postgres.sql
var postgresRestoreDDL string

//go:embed 003_sqlite.sql
var sqliteRestoreSourceDDL string

//go:embed 003_postgres.sql
var postgresRestoreSourceDDL string

const storeIdentityBytes = 16

// Migration revision 2 adds private recovery bookkeeping. It does not change
// the canonical record representation or store_meta's archive schema version 1.
const restoreMigrationVersion = 2

const restoreSourceMigrationVersion = 3

// Migrate applies the embedded initial additive schema transactionally. The
// owner must hold exclusive store ownership. Unknown history is never repaired.
//
//nolint:cyclop // All three additive migrations share the atomic ownership transaction.
func (c *Connection) Migrate(ctx context.Context) error {
	return c.run(ctx, true, false, func(tx *Tx) error {
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

		restoreDDL := sqliteRestoreDDL
		if c.pg != nil {
			restoreDDL = postgresRestoreDDL
		}

		restoreChecksum := migrationChecksum(restoreDDL)

		sourceDDL := sqliteRestoreSourceDDL
		if c.pg != nil {
			sourceDDL = postgresRestoreSourceDDL
		}

		sourceChecksum := migrationChecksum(sourceDDL)

		applied, err := c.checkMigration(ctx, tx.q, []string{checksum, restoreChecksum, sourceChecksum})
		if err != nil {
			return err
		}

		if applied > 0 {
			if err := c.checkMetadata(ctx, tx.q); err != nil {
				return err
			}
		} else if err := tx.initialize(ctx, ddl, checksum); err != nil {
			return err
		}

		if applied < restoreMigrationVersion {
			if err := tx.applyAdditiveMigration(ctx, restoreDDL, restoreChecksum, restoreMigrationVersion); err != nil {
				return err
			}
		}

		if applied < restoreSourceMigrationVersion {
			if err := tx.applyAdditiveMigration(ctx, sourceDDL, sourceChecksum, restoreSourceMigrationVersion); err != nil {
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

func (tx *Tx) applyAdditiveMigration(ctx context.Context, ddl, checksum string, version int) error {
	if err := applyDDL(ctx, tx.q, ddl); err != nil {
		return err
	}

	_, err := tx.q.exec(ctx, "INSERT INTO "+tx.connection.table("schema_migration")+
		" (version,checksum) VALUES($1,$2)", version, checksum)

	return databaseError(ctx, "record additive migration", err)
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

func (c *Connection) checkMetadata(ctx context.Context, q query) error {
	var (
		identity, clock []byte
		schema, privacy int
	)

	err := scanOne(ctx, q, "SELECT store_uuid,mutation_clock,schema_version,privacy_protocol FROM "+
		c.table("store_meta")+" WHERE singleton=1",
		nil, &identity, &clock, &schema, &privacy)
	if err != nil {
		return databaseError(ctx, "read metadata", err)
	}

	if len(identity) != storeIdentityBytes || len(clock) != unsignedBytes || schema != 1 ||
		privacy != 1 {
		return invalid("invalid canonical SQL metadata")
	}

	return nil
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

//nolint:mnd // UUID version 4 and RFC variant bit positions/masks.
func newStoreUUID() ([]byte, error) {
	identity := make([]byte, storeIdentityBytes)
	if _, err := rand.Read(identity); err != nil {
		return nil, invalid("canonical SQL identity unavailable")
	}

	identity[6] = (identity[6] & 15) | 64
	identity[8] = (identity[8] & 63) | 128

	return identity, nil
}

func (tx *Tx) initialize(ctx context.Context, ddl, checksum string) error {
	if err := applyDDL(ctx, tx.q, ddl); err != nil {
		return err
	}

	identity, err := newStoreUUID()
	if err != nil {
		return err
	}

	if _, err := tx.q.exec(ctx, "INSERT INTO "+tx.connection.table("store_meta")+
		" (singleton,store_uuid,schema_version,privacy_protocol,mutation_clock) VALUES(1,$1,1,1,$2)",
		identity, Uint64(0)); err != nil {
		return databaseError(ctx, "initialize metadata", err)
	}

	_, err = tx.q.exec(ctx, "INSERT INTO "+tx.connection.table("schema_migration")+
		" (version,checksum) VALUES(1,$1)", checksum)
	if err != nil {
		return databaseError(ctx, "record migration", err)
	}

	return tx.CheckIntegrity(ctx)
}
