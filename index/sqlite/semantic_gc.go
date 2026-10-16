//nolint:lll // Fixed bounded deletion SQL retains the exact ownership relation at each mutation.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	semantic "github.com/baldaworks/boyan/internal/semantic"
)

// CollectGarbage preserves live stages and complete unpublished pending builds.
func (store *Store) CollectGarbage(ctx context.Context) error {
	return store.collectSemanticGarbage(ctx, false)
}

// RepairSemanticOrphans removes nonactive abandoned builds before workers start.
// The manager must call this explicitly; its failure is semantic-only recovery.
func (store *Store) RepairSemanticOrphans(ctx context.Context) error {
	return store.collectSemanticGarbage(ctx, true)
}

func (store *Store) collectSemanticGarbage(ctx context.Context, startup bool) error {
	if ctx == nil {
		return semantic.ErrInvalid
	}

	operation, done, err := store.acquire(ctx)
	if err != nil {
		return semanticStorageError(err)
	}
	defer done()

	if !store.embeddingsEnabled {
		return operation.Err()
	}

	var after int64

	for {
		var rowid int64

		err = store.writer.QueryRowContext(operation, `SELECT b.rowid FROM semantic_build b
   WHERE b.rowid>? AND NOT EXISTS(SELECT 1 FROM semantic_active a WHERE a.build_id=b.build_id)
   AND (? OR b.complete=0 OR b.epoch!=?) ORDER BY b.rowid LIMIT 1`, after, startup, encodeUint64(0)).Scan(&rowid)
		if errors.Is(err, sql.ErrNoRows) {
			return operation.Err()
		}

		if err != nil {
			return semanticStorageError(err)
		}

		after = rowid
		if err = store.deleteSemanticOrphan(operation, rowid, startup); err != nil {
			return semanticStorageError(err)
		}
	}
}

// Each transaction retires at most 128 owned chunk/coverage rows. FK membership
// fanout is cooperative SQL work, not a byte or wall-clock bound. Recheck active
// and Stage ownership inside every writer transaction before removing anything.
//
//nolint:cyclop // Every page rechecks ownership, active state and both row families before final retirement.
func (store *Store) deleteSemanticOrphan(ctx context.Context, rowid int64, startup bool) error {
	for {
		finished := false

		err := store.write(ctx, func(ctx context.Context, tx *sql.Tx) error {
			var build sql.NullString

			err := tx.QueryRowContext(ctx, `SELECT CASE WHEN length(CAST(b.build_id AS BLOB))<=512 THEN b.build_id END FROM semantic_build b WHERE b.rowid=?
    AND NOT EXISTS(SELECT 1 FROM semantic_active a WHERE a.build_id=b.build_id)
    AND (? OR b.complete=0 OR b.epoch!=?)`, rowid, startup, encodeUint64(0)).Scan(&build)
			if errors.Is(err, sql.ErrNoRows) {
				finished = true

				return nil
			}

			if err != nil {
				return err
			}

			if _, live := store.semanticStaging.Load(build.String); build.Valid && live {
				finished = true

				return nil
			}

			for _, query := range []string{
				`DELETE FROM semantic_chunk WHERE rowid IN (SELECT rowid FROM semantic_chunk WHERE build_id=(SELECT build_id FROM semantic_build WHERE rowid=?) ORDER BY rowid LIMIT 128)`,
				`DELETE FROM semantic_coverage WHERE rowid IN (SELECT rowid FROM semantic_coverage WHERE build_id=(SELECT build_id FROM semantic_build WHERE rowid=?) AND NOT EXISTS(SELECT 1 FROM semantic_chunk c WHERE c.build_id=semantic_coverage.build_id AND c.revision_id=semantic_coverage.revision_id) ORDER BY rowid LIMIT 128)`,
			} {
				result, execErr := tx.ExecContext(ctx, query, rowid)
				if execErr != nil {
					return execErr
				}

				n, execErr := result.RowsAffected()
				if execErr != nil {
					return execErr
				}

				if n > 0 {
					return nil
				}
			}

			_, err = tx.ExecContext(ctx, "DELETE FROM semantic_build WHERE rowid=?", rowid)
			finished = true

			return err
		})
		if err != nil {
			return err
		}

		if finished {
			return nil
		}
	}
}
