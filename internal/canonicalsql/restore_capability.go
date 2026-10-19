package canonicalsql

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/baldaworks/boyan/store/archive"
)

func (tx *Tx) preserveRestoreCapability(ctx context.Context, key Key, record archive.BackupRecord) error {
	if record.Deleted {
		return nil
	}

	found, err := loadBoundaryCapability(ctx, tx, key.Scope)
	if err != nil || found {
		return err
	}

	legacy, err := tx.HasLegacyOperationNamespace(ctx, key.Scope)
	if err != nil {
		return err
	}

	if legacy {
		return boundaryIdentityConflict()
	}

	return tx.Put(ctx, key, record.Envelope)
}

func (tx *Tx) checkRestoreOperationSource(ctx context.Context, key Key, generation string) error {
	if key.Family != Operation || len(key.Parts[0]) != len(legacyDerivedOperationPrefix)+legacyDerivedOperationHashBytes ||
		!strings.HasPrefix(string(key.Parts[0]), legacyDerivedOperationPrefix) {
		return nil
	}

	marked, err := loadBoundaryCapability(ctx, tx, key.Scope)
	if err != nil {
		return err
	}

	positive, err := tx.hasRestoreSource(ctx, generation, key.Scope)
	if err != nil {
		return err
	}

	if marked != positive {
		return boundaryIdentityConflict()
	}

	return nil
}

// The same pass runs in rollback validation and in bounded committed replay.
// Candidates are checked against pre-existing history before any operation is added.
func (tx *Tx) restoreCapabilityStream(ctx context.Context, spool io.Reader, limit int) error {
	for n := 0; limit == 0 || n < limit; n++ {
		data, err := readRestoreFrame(ctx, spool)
		if err != nil {
			return err
		}

		record, key, err := decodeRestoreRecord(data)
		if err != nil {
			return err
		}

		if key.Family == BoundaryCapability {
			if err := tx.preserveRestoreCapability(ctx, key, record); err != nil {
				return err
			}
		}

		tx.scopeStamps = nil
	}

	return nil
}

func (o *Owner) replayRestoreCapabilities(ctx context.Context, spool io.ReadSeeker, pending int) error {
	for {
		finished := false

		err := o.restoreTransaction(ctx, true, func(ctx context.Context, tx *Tx) error {
			err := tx.restoreCapabilityStream(ctx, spool, pending)
			if errors.Is(err, io.EOF) {
				finished = true

				return nil
			}

			return err
		})
		if err != nil {
			return err
		}

		if finished {
			_, err := spool.Seek(0, io.SeekStart)

			return err
		}
	}
}
