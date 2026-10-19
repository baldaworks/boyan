package canonicalsql

import (
	"context"
	"errors"
	"io"
	"os"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/archive"
)

const maxRestoreBatch = 4096

// RestoreCanonicalBackup validates and replays a complete opaque artifact
// behind a durable fence. Failure retains control across owner reopening.
func (o *Owner) RestoreCanonicalBackup(
	ctx context.Context,
	source io.Reader,
	pending int,
) (result error) {
	return o.restoreCanonical(ctx, source, pending, nativeRestoreFormat, stageRestore, nil)
}

type restoreFormat uint8

const (
	nativeRestoreFormat restoreFormat = iota
	logicalRestoreFormat
)

type restoreStager func(context.Context, io.Reader, bool) (*os.File, archive.BackupHeader, string, error)

// restoreCanonical is the single loading/validation/merge/privacy authority for
// both supported SQL formats. A logical stager produces the same private spool.
//
//nolint:cyclop,gocognit,funlen // Ordered durable transitions and resource cleanup remain explicit.
func (o *Owner) restoreCanonical(
	ctx context.Context,
	source io.Reader,
	pending int,
	format restoreFormat,
	stage restoreStager,
	progress func(archive.CanonicalLogicalImportProgress) error,
) (result error) {
	if err := checkArchiveContext(ctx); err != nil {
		return err
	}

	if source == nil || nilBackupStream(source) {
		return invalid("nil canonical backup source")
	}

	if pending <= 0 {
		pending = 128
	}

	if pending > maxRestoreBatch {
		return limitError()
	}

	admitted, release, err := o.admit(ctx)
	if err != nil {
		return err
	}
	defer release()
	defer func() {
		if err := checkArchiveContext(admitted); err != nil {
			result = err

			return
		}

		if _, _, classified := memory.ClassifyError(result); result != nil && !classified {
			// Caller reader/filesystem diagnostics may contain content or paths.
			// Preserve the original maintenance retry class without exposing them.
			result = memory.RetryableError(
				memory.CodeStoreFailure,
				"canonical SQL restore stream failed",
				nil,
			)
		}
	}()

	var previous, chain *restoreControl

	err = o.restoreTransaction(admitted, true, func(ctx context.Context, tx *Tx) error {
		var err error

		previous, err = tx.readRestoreControl(ctx, "restore_control")
		if err != nil {
			return err
		}

		chain, err = tx.readRestoreControl(ctx, "restore_chain")
		if err != nil {
			return err
		}

		if previous != nil {
			return nil
		}

		if err := tx.validateRestoreErase(ctx); err != nil {
			return err
		}

		// Fresh logical import cannot acquire payload authority while a live
		// destination erase owns it. Existing loading retries and native recovery
		// retain their original admission; incoming pending journals are validated
		// later against the staged artifact, not rejected by this destination guard.
		if format == logicalRestoreFormat {
			if err := checkScopeErasePayloadGuard(ctx, tx); err != nil {
				return err
			}
		}

		if err := tx.verifyCanonicalIntegrity(ctx, archivePageSize); err != nil {
			return err
		}

		return tx.writeRestoreControl(
			ctx,
			"restore_control",
			restoreControl{Version: restoreControlVersion, Phase: "loading"},
		)
	})
	if err != nil {
		return err
	}

	spool, header, digest, err := stage(admitted, source, o.connection.pg != nil)
	if err != nil {
		return err
	}

	defer func() { result = errors.Join(result, spool.Close()) }()

	if err := validateRestoreChain(header, digest, previous, chain); err != nil {
		return err
	}

	control, err := o.prepareRestoreSource(admitted, spool, header, digest, previous, chain)
	if err != nil {
		return err
	}

	if err := o.validateRestoreInput(admitted, spool, control.PendingGeneration); err != nil {
		return o.retireRejectedRestore(admitted, previous, control, chain, err)
	}

	if err := o.replayRestoreCapabilities(admitted, spool, pending); err != nil {
		return err
	}

	if err := o.replayRestoreProgress(admitted, spool, pending, progress, control.PendingGeneration); err != nil {
		return err
	}

	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		return err
	}

	if err := o.replayRestoreCleanup(admitted, spool, pending); err != nil {
		return err
	}

	control.Phase = "validated"

	if err := o.restoreTransaction(admitted, true, func(ctx context.Context, tx *Tx) error {
		if err := tx.restoreEraseGuard(ctx); err != nil {
			return err
		}

		if err := tx.validateRestoreErase(ctx); err != nil {
			return err
		}

		if err := tx.verifyCanonicalIntegrity(ctx, archivePageSize); err != nil {
			return err
		}

		return tx.writeRestoreControl(ctx, "restore_control", control)
	}); err != nil {
		return err
	}

	return o.finishRestore(admitted, control)
}

//nolint:cyclop // Keep the complete lineage and retry identity predicate together.
func validateRestoreChain(
	h archive.BackupHeader,
	digest string,
	previous, chain *restoreControl,
) error {
	if previous != nil && previous.Version == 1 && previous.Header != nil {
		return invalid("legacy partial canonical restore lacks source provenance")
	}

	if previous != nil && previous.Header != nil &&
		(*previous.Header != h || previous.Digest != digest) {
		return invalid("complete matching canonical restore retry required")
	}

	if chain == nil {
		if h.Mode != "full" {
			return invalid("canonical restore requires full base")
		}

		return nil
	}

	if chain.Header == nil || chain.Phase != "complete" {
		return invalid("invalid canonical restore lineage")
	}

	old := chain.Header
	if old.SourceStoreUUID != h.SourceStoreUUID || old.Backend != h.Backend || h.End < old.End ||
		(h.Mode == "incremental" && h.Base != old.End) {
		return invalid("canonical restore lineage or cursor mismatch")
	}

	if h.Mode == "incremental" && (chain.Version != 2 || chain.ActiveGeneration == "") {
		return invalid("canonical restore source provenance missing; complete full base required")
	}

	return nil
}

//nolint:cyclop // Durable progress runs only after each successful bounded transaction.
func (o *Owner) replayRestoreProgress(ctx context.Context, source io.Reader, pending int,
	progress func(archive.CanonicalLogicalImportProgress) error,
	generation string,
) error {
	var imported uint64

	for {
		finished := false

		var batch uint64

		err := o.restoreTransaction(ctx, true, func(ctx context.Context, tx *Tx) error {
			for range pending {
				data, err := readRestoreFrame(ctx, source)
				if errors.Is(err, io.EOF) {
					finished = true

					return nil
				}

				if err != nil {
					return err
				}

				record, key, err := decodeRestoreRecord(data)
				if err != nil {
					return err
				}

				if err := tx.mergeRestoreRecord(ctx, key, record, generation); err != nil {
					return err
				}

				batch++
			}

			return nil
		})
		if err != nil {
			return err
		}

		imported += batch
		if batch != 0 && progress != nil {
			if err := progress(archive.CanonicalLogicalImportProgress{ImportedRecords: imported}); err != nil {
				return err
			}
		}

		if finished {
			return nil
		}
	}
}

//nolint:cyclop // Recheck lineage and integrity on both sides of privacy preparation before atomic publication.
func (o *Owner) finishRestore(ctx context.Context, control restoreControl) error {
	if control.Version != 2 || control.SourceState != "ready" || control.Phase != "validated" {
		return ownerUnavailable()
	}

	if err := o.restoreTransaction(ctx, false, func(ctx context.Context, tx *Tx) error {
		if err := tx.validateRestoreSourceLineage(ctx, control); err != nil {
			return err
		}

		if err := tx.validateRestoreErase(ctx); err != nil {
			return err
		}

		return tx.verifyCanonicalIntegrity(ctx, archivePageSize)
	}); err != nil {
		return err
	}
	// This capability is carried only by the held validated restore context.
	// Ordinary preparation still checks both durable loading and erase fences.
	ctx = context.WithValue(ctx, validatedRestoreKey{}, o)
	if err := o.prepareSourcePrivacy(ctx, true); err != nil {
		return err
	}

	err := o.restoreTransaction(ctx, true, func(ctx context.Context, tx *Tx) error {
		if err := tx.validateRestoreSourceLineage(ctx, control); err != nil {
			return err
		}

		if err := tx.validateRestoreErase(ctx); err != nil {
			return err
		}

		if err := tx.verifyCanonicalIntegrity(ctx, archivePageSize); err != nil {
			return err
		}

		control.Phase = "complete"
		control.ActiveGeneration = control.PendingGeneration

		control.PendingGeneration = ""
		if err := tx.writeRestoreControl(ctx, "restore_chain", control); err != nil {
			return err
		}

		_, err := tx.q.exec(
			ctx,
			"DELETE FROM "+tx.connection.table("restore_control")+" WHERE singleton=1",
		)

		return databaseError(ctx, "finish canonical restore", err)
	})
	if err != nil {
		return err
	}
	// Authority is already atomically published. Failed cleanup may safely lag.
	_ = o.collectRestoreSources(ctx)

	return nil
}

type validatedRestoreKey struct{}

var errRestoreValidationRollback = errors.New("canonical restore validation rollback")

// validateRestoreInput composes all stored cross-record validators against the
// merged result without committing any archive row. Native transaction/WAL
// space can grow with input; resident application memory stays page bounded.
// Only the subsequent replay commits maxPendingWrites-sized transactions.
//
//nolint:cyclop,gocognit,funlen // All artifact passes and binding checks share one rollback-only transaction.
func (o *Owner) validateRestoreInput(ctx context.Context, spool io.ReadSeeker, generation string) error {
	var technicalFailure bool

	spool = restoreValidationStream{ReadSeeker: spool, failed: &technicalFailure}

	err := o.restoreValidationTransaction(ctx, &technicalFailure, func(ctx context.Context, tx *Tx) error {
		if err := tx.restoreCapabilityStream(ctx, spool, 0); err != nil && !errors.Is(err, io.EOF) {
			return err
		}

		if _, err := spool.Seek(0, io.SeekStart); err != nil {
			return err
		}

		for {
			data, err := readRestoreFrame(ctx, spool)
			if errors.Is(err, io.EOF) {
				break
			}

			if err != nil {
				return err
			}

			record, key, err := decodeRestoreRecord(data)
			if err != nil {
				return err
			}

			if err := tx.mergeRestoreRecord(ctx, key, record, generation); err != nil {
				return err
			}
			// No normal domain operation occurs inside this validation pass.
			// Avoid retaining one scope entry per archived scope in Go memory.
			tx.scopeStamps = nil
		}

		if _, err := spool.Seek(0, io.SeekStart); err != nil {
			return err
		}

		if err := tx.restoreCleanupStream(ctx, spool, 0); err != nil && !errors.Is(err, io.EOF) {
			return err
		}

		if err := tx.restoreEraseGuard(ctx); err != nil {
			return err
		}

		// All writes are now complete. Read validators use the native clock,
		// including this transaction's stamps, without a writer's lazy-clock guard.
		tx.sequence = nil
		if err := tx.validateRestoreErase(ctx); err != nil {
			return err
		}

		if err := tx.verifyCanonicalIntegrity(ctx, archivePageSize); err != nil {
			return err
		}

		return errRestoreValidationRollback
	})
	if err != errRestoreValidationRollback {
		return err
	}

	_, err = spool.Seek(0, io.SeekStart)

	return err
}
