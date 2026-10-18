package canonicalsql

import (
	"context"
	"encoding/hex"
	"encoding/json"

	"github.com/baldaworks/boyan/store/archive"
)

type restoreControl struct {
	Version int                   `json:"version"`
	Phase   string                `json:"phase"`
	Header  *archive.BackupHeader `json:"header,omitempty"`
	Digest  string                `json:"digest,omitempty"`
}

// restoreTransaction is private to held restore admission. It rechecks the
// pinned session for every transaction, including control-only commits.
func (o *Owner) restoreTransaction(
	ctx context.Context,
	write bool,
	fn func(context.Context, *Tx) error,
) error {
	if err := o.checkPostgresOwnership(ctx); err != nil {
		return err
	}
	defer o.fenceBrokenPostgres()

	return o.connection.run(ctx, write, false, func(tx *Tx) error { return fn(ctx, tx) })
}

//nolint:cyclop // A durable phase requires every version, lineage and digest check.
func (tx *Tx) readRestoreControl(ctx context.Context, table string) (*restoreControl, error) {
	r, err := tx.q.query(
		ctx,
		"SELECT control FROM "+tx.connection.table(table)+" WHERE singleton=1",
	)
	if err != nil {
		return nil, databaseError(ctx, "read restore control", err)
	}
	defer r.Close()

	if !r.Next() {
		return nil, r.Err()
	}

	var data []byte
	if err := r.Scan(&data); err != nil {
		return nil, databaseError(ctx, "read restore control", err)
	}

	var control restoreControl
	if len(data) > 4096 || decodeRestoreJSON(data, &control) != nil || control.Version != 1 ||
		(control.Phase != "loading" && control.Phase != "validated" && control.Phase != "complete") {
		return nil, invalid("invalid canonical restore control")
	}

	if control.Header != nil {
		if err := validateRestoreHeader(*control.Header, tx.connection.pg != nil); err != nil {
			return nil, err
		}

		digest, err := hex.DecodeString(control.Digest)
		if err != nil || len(digest) != 32 {
			return nil, invalid("invalid canonical restore control digest")
		}
	} else if control.Phase != "loading" || control.Digest != "" {
		return nil, invalid("invalid canonical restore phase")
	}

	return &control, nil
}

func (tx *Tx) writeRestoreControl(ctx context.Context, table string, control restoreControl) error {
	encoded, err := json.Marshal(control)
	if err != nil {
		return invalid("invalid canonical restore control")
	}

	_, err = tx.q.exec(ctx, "INSERT INTO "+tx.connection.table(table)+
		" (singleton,control) VALUES(1,$1) ON CONFLICT(singleton) DO UPDATE SET control=excluded.control", encoded)

	return databaseError(ctx, "write restore control", err)
}

// ResumeRestoreBeforeEraseRecovery resumes only trusted validated state.
// Loading requires the complete matching artifact, never ordinary preparation.
func (o *Owner) ResumeRestoreBeforeEraseRecovery(ctx context.Context) error {
	if err := checkArchiveContext(ctx); err != nil {
		return err
	}

	admitted, release, err := o.admit(ctx)
	if err != nil {
		return err
	}
	defer release()

	return o.resumeRestore(admitted)
}

func (o *Owner) resumeRestore(ctx context.Context) error {
	var control *restoreControl

	err := o.restoreTransaction(ctx, false, func(ctx context.Context, tx *Tx) error {
		var err error

		control, err = tx.readRestoreControl(ctx, "restore_control")

		return err
	})
	if err != nil || control == nil {
		return err
	}

	if control.Phase != "validated" || control.Header == nil {
		return ownerUnavailable()
	}

	return o.finishRestore(ctx, *control)
}
