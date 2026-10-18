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

const storeIdentityBytes = 16

// Migrate applies the embedded initial additive schema transactionally. The
// owner must hold exclusive store ownership. Unknown history is never repaired.
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

		digest := sha256.Sum256([]byte(ddl))
		checksum := hex.EncodeToString(digest[:])

		applied, err := c.checkMigration(ctx, tx.q, checksum)
		if err != nil {
			return err
		}

		if applied {
			if err := c.checkMetadata(ctx, tx.q); err != nil {
				return err
			}

			return tx.CheckIntegrity(ctx)
		}

		return tx.initialize(ctx, ddl, checksum)
	})
}

func (c *Connection) checkMigration(ctx context.Context, q query, checksum string) (bool, error) {
	r, err := q.query(ctx, "SELECT version,checksum FROM "+c.table("schema_migration")+" ORDER BY version")
	if err != nil {
		return false, databaseError(ctx, "read migration", err)
	}
	defer r.Close()

	applied := false

	for r.Next() {
		var (
			version int
			stored  string
		)
		if err := r.Scan(&version, &stored); err != nil {
			return false, databaseError(ctx, "read migration", err)
		}

		if version != 1 || stored != checksum || applied {
			return false, invalid("unsupported canonical SQL schema history")
		}

		applied = true
	}

	if err := r.Err(); err != nil {
		return false, databaseError(ctx, "read migration", err)
	}

	if err := r.Close(); err != nil {
		return false, databaseError(ctx, "close migration rows", err)
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

	if len(identity) != storeIdentityBytes || len(clock) != unsignedBytes || schema != 1 || privacy != 1 {
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
