package canonicalsql

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"strconv"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/archive"
)

// A semantic rejection grants retirement only after the exact callback error
// survives rollback. A joined rollback error, cancellation, or observed technical
// error cannot create this private outcome, even when a validator hides its cause.
type restoreInputRejection struct{ cause error }

func (e *restoreInputRejection) Error() string { return e.cause.Error() }
func (e *restoreInputRejection) Unwrap() error { return e.cause }

func (o *Owner) restoreValidationTransaction(ctx context.Context, failed *bool,
	validate func(context.Context, *Tx) error,
) error {
	var callbackError error

	result := o.restoreTransaction(ctx, true, func(ctx context.Context, tx *Tx) error {
		tx.q = restoreValidationQuery{base: tx.q, failed: failed}
		callbackError = validate(ctx, tx)

		return callbackError
	})

	_, class, classified := memory.ClassifyError(callbackError)
	if result != nil && result == callbackError && ctx.Err() == nil && !*failed &&
		classified && class == memory.ErrorClassPermanent {
		return &restoreInputRejection{cause: callbackError}
	}

	return result
}

//nolint:cyclop // Retirement permission and exact transactional binding checks form one fail-closed transition.
func (o *Owner) retireRejectedRestore(ctx context.Context, previous *restoreControl,
	expected restoreControl, chain *restoreControl, rejected error,
) error {
	if _, confirmed := rejected.(*restoreInputRejection); !confirmed || ctx.Err() != nil {
		return rejected
	}
	// A ready/validated entry may have committed replay in an earlier invocation.
	if previous != nil && (previous.Phase != "loading" ||
		(previous.Header != nil && (previous.Version != 2 || previous.SourceState != "building"))) {
		return rejected
	}

	err := o.restoreTransaction(ctx, true, func(ctx context.Context, tx *Tx) error {
		current, err := tx.readRestoreControl(ctx, "restore_control")
		if err != nil {
			return err
		}

		accepted, err := tx.readRestoreControl(ctx, "restore_chain")
		if err != nil {
			return err
		}

		if expected.Phase != "loading" || expected.SourceState != "ready" ||
			!sameRestoreControl(current, &expected) || !sameRestoreControl(accepted, chain) {
			return invalid("canonical rejected restore binding changed")
		}
		// Keep the fence. Ready rows are immutable and now merely unreferenced;
		// a replacement allocates a different generation, and bounded GC may lag.
		return tx.writeRestoreControl(ctx, "restore_control",
			restoreControl{Version: restoreControlVersion, Phase: "loading"})
	})

	return errors.Join(rejected, err)
}

func sameRestoreControl(a, b *restoreControl) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}

	x, y := *a, *b
	x.Header, y.Header = nil, nil

	return x == y && ((a.Header == nil && b.Header == nil) ||
		(a.Header != nil && b.Header != nil && *a.Header == *b.Header))
}

// These observers exist only for rollback validation, not normal transactions.
// They preserve native results and remember technical failures before existing
// semantic validators can replace an error (notably privacy-binding failures).
type restoreValidationQuery struct {
	base   query
	failed *bool
}

func (q restoreValidationQuery) exec(ctx context.Context, statement string, args ...any) (int64, error) {
	n, err := q.base.exec(ctx, statement, args...)
	if err != nil {
		*q.failed = true
	}

	return n, err
}

func (q restoreValidationQuery) query(ctx context.Context, statement string, args ...any) (rows, error) {
	r, err := q.base.query(ctx, statement, args...)
	if err != nil {
		*q.failed = true

		return r, err
	}

	return restoreValidationRows{rows: r, failed: q.failed}, nil
}

type restoreValidationRows struct {
	rows

	failed *bool
}

func (r restoreValidationRows) Next() bool {
	next := r.rows.Next()
	if !next && r.rows.Err() != nil {
		*r.failed = true
	}

	return next
}

func (r restoreValidationRows) Scan(dest ...any) error {
	err := r.rows.Scan(dest...)
	if err != nil {
		*r.failed = true
	}

	return err
}

func (r restoreValidationRows) Err() error {
	err := r.rows.Err()
	if err != nil {
		*r.failed = true
	}

	return err
}

func (r restoreValidationRows) Close() error {
	err := r.rows.Close()
	if err != nil {
		*r.failed = true
	}

	return err
}

type restoreValidationStream struct {
	io.ReadSeeker

	failed *bool
}

func (s restoreValidationStream) Read(p []byte) (int, error) {
	n, err := s.ReadSeeker.Read(p)
	if err != nil && err != io.EOF {
		*s.failed = true
	}

	return n, err
}

func (s restoreValidationStream) Seek(offset int64, whence int) (int64, error) {
	n, err := s.ReadSeeker.Seek(offset, whence)
	if err != nil {
		*s.failed = true
	}

	return n, err
}

// prepareRestoreSource never reads destination capability rows. Ready generations
// are immutable; an interrupted building generation is rebuilt from its bound input.
//
//nolint:cyclop,funlen // Keep allocation, retry identity, building, and ready publication in protocol order.
func (o *Owner) prepareRestoreSource(ctx context.Context, spool io.ReadSeeker,
	header archive.BackupHeader, digest string, previous, chain *restoreControl,
) (restoreControl, error) {
	control := restoreControl{Version: restoreControlVersion, Phase: "loading", Header: &header,
		Digest: digest, SourceState: "building"}
	if chain != nil {
		control.ActiveGeneration = chain.ActiveGeneration
	}

	if previous != nil && previous.Header != nil {
		control = *previous
		if control.ActiveGeneration != activeRestoreGeneration(chain) {
			return control, invalid("canonical restore source lineage mismatch")
		}
	} else {
		identity, err := newStoreUUID()
		if err != nil {
			return control, err
		}

		control.PendingGeneration = hex.EncodeToString(identity)
		if err := o.restoreTransaction(ctx, true, func(ctx context.Context, tx *Tx) error {
			var count int
			if err := scanOne(ctx, tx.q, "SELECT count(*) FROM "+tx.connection.table("restore_source_capability")+
				" WHERE generation=$1", []any{identity}, &count); err != nil {
				return databaseError(ctx, "allocate restore source generation", err)
			}

			if count != 0 || control.PendingGeneration == control.ActiveGeneration {
				return invalid("canonical restore source generation collision")
			}

			return tx.writeRestoreControl(ctx, "restore_control", control)
		}); err != nil {
			return control, err
		}
	}

	if control.SourceState == "ready" {
		return control, nil
	}

	if err := o.clearRestoreSource(ctx, control.PendingGeneration); err != nil {
		return control, err
	}

	if header.Mode == "incremental" {
		if err := o.copyRestoreSource(ctx, control.ActiveGeneration, control.PendingGeneration); err != nil {
			return control, err
		}
	}

	if err := o.applyRestoreSource(ctx, spool, control.PendingGeneration); err != nil {
		return control, err
	}

	control.SourceState = "ready"
	err := o.restoreTransaction(ctx, true, func(ctx context.Context, tx *Tx) error {
		return tx.writeRestoreControl(ctx, "restore_control", control)
	})

	return control, err
}

func activeRestoreGeneration(chain *restoreControl) string {
	if chain == nil {
		return ""
	}

	return chain.ActiveGeneration
}

func (o *Owner) clearRestoreSource(ctx context.Context, generation string) error {
	gen, err := sourceGenerationBytes(generation)
	if err != nil {
		return err
	}

	for {
		var count int

		err := o.restoreTransaction(ctx, true, func(ctx context.Context, tx *Tx) error {
			page, err := tx.restoreSourcePage(ctx, "generation=$1", []any{gen})
			if err != nil {
				return err
			}

			count = len(page)

			return tx.deleteRestoreSourceRows(ctx, page)
		})
		if err != nil || count == 0 {
			return err
		}
	}
}

func (o *Owner) copyRestoreSource(ctx context.Context, active, pending string) error {
	gen, err := sourceGenerationBytes(active)
	if err != nil {
		return err
	}

	var after int64

	for {
		var count int

		err := o.restoreTransaction(ctx, true, func(ctx context.Context, tx *Tx) error {
			page, err := tx.restoreSourcePage(ctx, "generation=$1 AND row_id>$2", []any{gen, after})
			if err != nil {
				return err
			}

			count = len(page)
			for _, row := range page {
				if err := tx.putRestoreSource(ctx, pending, row.scope, true); err != nil {
					return err
				}

				after = row.id
			}

			return nil
		})
		if err != nil || count == 0 {
			return err
		}
	}
}

//nolint:cyclop,gocognit // The bounded transaction keeps source-only decoding and additions/tombstones atomic.
func (o *Owner) applyRestoreSource(ctx context.Context, spool io.ReadSeeker, generation string) error {
	for {
		finished := false

		err := o.restoreTransaction(ctx, true, func(ctx context.Context, tx *Tx) error {
			for range archivePageSize {
				data, err := readRestoreFrame(ctx, spool)
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

				if key.Family != BoundaryCapability {
					continue
				}

				if record.Deleted {
					err = tx.deleteRestoreSource(ctx, generation, key.Scope)
				} else {
					err = tx.putRestoreSource(ctx, generation, key.Scope, false)
				}

				if err != nil {
					return err
				}
			}

			return nil
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

// Cleanup can lag safely. References are reread inside each deletion transaction.
//
//nolint:cyclop,gocognit // Read both controls and exclude referenced generations in each bounded deletion transaction.
func (o *Owner) collectRestoreSources(ctx context.Context) error {
	for {
		var count int

		err := o.restoreTransaction(ctx, true, func(ctx context.Context, tx *Tx) error {
			where := "1=1"

			var args []any

			for _, table := range []string{"restore_chain", "restore_control"} {
				control, err := tx.readRestoreControl(ctx, table)
				if err != nil {
					return err
				}

				if control == nil {
					continue
				}

				for _, generation := range []string{control.ActiveGeneration, control.PendingGeneration} {
					if generation == "" {
						continue
					}

					gen, err := sourceGenerationBytes(generation)
					if err != nil {
						return err
					}

					args = append(args, gen)
					where += " AND generation<>$" + strconv.Itoa(len(args))
				}
			}

			page, err := tx.restoreSourcePage(ctx, where, args)
			if err != nil {
				return err
			}

			count = len(page)

			return tx.deleteRestoreSourceRows(ctx, page)
		})
		if err != nil || count == 0 {
			return err
		}
	}
}
