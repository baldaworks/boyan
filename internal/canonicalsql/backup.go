package canonicalsql

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"hash"
	"io"
	"reflect"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/archive"
)

const archivePageSize = 32

// WriteCanonicalBackup holds admission and one read snapshot for the complete
// stream. Stamps and live values cannot straddle a normal write or maintenance.
func (o *Owner) WriteCanonicalBackup(
	ctx context.Context,
	destination io.Writer,
	since uint64,
) (uint64, error) {
	if err := checkArchiveContext(ctx); err != nil {
		return 0, err
	}

	if destination == nil || nilBackupStream(destination) {
		return 0, invalid("nil canonical backup destination")
	}

	var end uint64

	err := o.View(ctx, func(ctx context.Context, tx *Tx) error {
		var err error

		end, err = tx.writeCanonicalBackup(ctx, destination, since, archivePageSize)

		return err
	})
	if ctxErr := checkArchiveContext(ctx); ctxErr != nil {
		return 0, ctxErr
	}

	if err != nil {
		return 0, err
	}

	return end, nil
}

func checkArchiveContext(ctx context.Context) error {
	if ctx == nil {
		return invalid("canonical maintenance context is required")
	}

	if err := ctx.Err(); err != nil {
		return memory.RetryableError(memory.CodeTimeout, "canonical maintenance context ended", err)
	}

	return nil
}

func nilBackupStream(value any) bool {
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func (tx *Tx) writeCanonicalBackup(
	ctx context.Context,
	destination io.Writer,
	since uint64,
	pageSize int,
) (uint64, error) {
	if err := tx.verifyCanonicalIntegrity(ctx, pageSize); err != nil {
		return 0, err
	}

	header, err := tx.backupHeader(ctx, since)
	if err != nil {
		return 0, err
	}

	w := backupWriter{destination: destination, digest: sha256.New()}
	if err := w.frame(ctx, header, true); err != nil {
		return 0, err
	}

	var (
		after *StampCursor
		count uint64
	)

	for {
		page, err := tx.scanStampsSince(ctx, since, after, pageSize)
		if err != nil {
			return 0, err
		}

		if len(page) == 0 {
			break
		}

		for _, stamp := range page {
			if err := w.record(ctx, stamp); err != nil {
				return 0, err
			}

			count++
		}

		cursor := page[len(page)-1].Cursor
		after = &cursor
	}

	footer := archive.BackupFooter{
		Kind:        "footer",
		RecordCount: count,
		SHA256:      hex.EncodeToString(w.digest.Sum(nil)),
	}
	if err := w.frame(ctx, footer, false); err != nil {
		return 0, err
	}

	return header.End, nil
}

func (w *backupWriter) record(ctx context.Context, stamp Stamp) error {
	_, parts, err := stamp.Key.values()
	if err != nil {
		return err
	}

	record := archive.BackupRecord{
		Kind:       "record",
		Family:     string(stamp.Key.Family),
		Components: parts,
		Sequence:   stamp.Cursor.Sequence,
		Deleted:    stamp.Deleted,
		Envelope:   stamp.Envelope,
	}

	return w.frame(ctx, record, true)
}

func (tx *Tx) backupHeader(ctx context.Context, since uint64) (archive.BackupHeader, error) {
	h := archive.BackupHeader{
		Kind:          "header",
		Format:        archive.SQLBackupFormat,
		Backend:       "sqlite",
		ProofProtocol: 1,
		Mode:          "full",
		Base:          since,
	}
	if tx.connection.pg != nil {
		h.Backend = "postgres"
	}

	if since != 0 {
		h.Mode = "incremental"
	}

	var identity, clock []byte

	err := scanOne(
		ctx,
		tx.q,
		"SELECT store_uuid,mutation_clock,schema_version,privacy_protocol FROM "+tx.connection.table(
			"store_meta",
		)+" WHERE singleton=1",
		nil,
		&identity,
		&clock,
		&h.SchemaVersion,
		&h.PrivacyProtocol,
	)
	if err != nil {
		return h, databaseError(ctx, "backup metadata", err)
	}

	h.End, err = DecodeUint64(clock)
	if err != nil || len(identity) != storeIdentityBytes || h.SchemaVersion != 1 ||
		h.PrivacyProtocol != 1 {
		return h, corruptRow()
	}

	if since > h.End {
		return h, invalid("canonical backup cursor is ahead of store")
	}

	h.SourceStoreUUID = hex.EncodeToString(identity)

	return h, nil
}

type backupWriter struct {
	destination io.Writer
	digest      hash.Hash
}

// Values and tuple parts have already passed their smaller persistence limits
// before JSON allocation. The resulting frame is checked before any IO.
func (w *backupWriter) frame(ctx context.Context, value any, checksum bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		return invalid("canonical backup frame encoding failed")
	}

	if len(encoded) > archive.MaxOpaqueFrameBytes {
		return limitError()
	}

	var prefix [4]byte
	binary.BigEndian.PutUint32(prefix[:], uint32(len(encoded)))

	for _, part := range [][]byte{prefix[:], encoded} {
		if err := ctx.Err(); err != nil {
			return err
		}

		n, err := w.destination.Write(part)
		if err == nil && n != len(part) {
			err = io.ErrShortWrite
		}

		if err != nil {
			return memory.PermanentError(memory.CodeStoreFailure, "write canonical backup", err)
		}

		if checksum {
			_, _ = w.digest.Write(part)
		}
	}

	return ctx.Err()
}
