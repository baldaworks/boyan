package canonicalsql

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/archive"
)

// ExportCanonicalLogical holds one consistent snapshot and exclusive owner
// admission. It exports application tuple bytes and structural tombstones only.
//
//nolint:cyclop,funlen // Snapshot validation and bounded stream writes share one transaction.
func (o *Owner) ExportCanonicalLogical(ctx context.Context, destination io.Writer) (result error) {
	if err := checkArchiveContext(ctx); err != nil {
		return err
	}

	if destination == nil || nilBackupStream(destination) {
		return invalid("nil canonical logical destination")
	}

	defer func() { result = logicalBoundaryError(ctx, result) }()

	return o.View(ctx, func(ctx context.Context, tx *Tx) error {
		if err := tx.verifyCanonicalIntegrity(ctx, archivePageSize); err != nil {
			return err
		}

		h, err := tx.backupHeader(ctx, 0)
		if err != nil {
			return err
		}

		header := archive.CanonicalLogicalExportHeader{SchemaVersion: archive.SQLLogicalFormat, Kind: "header",
			SourceStoreUUID: h.SourceStoreUUID, Backend: h.Backend, StoreSchemaVersion: h.SchemaVersion,
			PrivacyProtocol: h.PrivacyProtocol, ProofProtocol: h.ProofProtocol, Mode: h.Mode, End: h.End}

		digest := sha256.New()
		if err := writeLogicalLine(ctx, io.MultiWriter(destination, digest), header); err != nil {
			return err
		}

		var (
			after *StampCursor
			count uint64
		)

		for {
			page, err := tx.ScanStamps(ctx, after, archivePageSize)
			if err != nil {
				return err
			}

			if len(page) == 0 {
				break
			}

			for _, stamp := range page {
				_, values, _ := stamp.Key.values()

				record := archive.CanonicalLogicalExportRecord{Kind: "record", RecordType: string(stamp.Key.Family),
					Components: make([]string, len(values)), Envelope: stamp.Envelope,
					Sequence: stamp.Cursor.Sequence, Deleted: stamp.Deleted}
				for i, value := range values {
					record.Components[i] = base64.StdEncoding.EncodeToString(value)
				}

				if err := writeLogicalLine(ctx, io.MultiWriter(destination, digest), record); err != nil {
					return err
				}

				count++
			}

			cursor := page[len(page)-1].Cursor
			after = &cursor
		}

		return writeLogicalLine(ctx, destination, archive.CanonicalLogicalExportFooter{
			SchemaVersion: archive.SQLLogicalFormat, Kind: "footer", RecordCount: count,
			SHA256: hex.EncodeToString(digest.Sum(nil))})
	})
}

//nolint:cyclop // Size checks precede allocation and each write checks cancellation and short writes.
func writeLogicalLine(ctx context.Context, destination io.Writer, value any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Compute exact base64 expansion before allocating a possibly oversized
	// encoded envelope. The small skeleton contains bounded tuple components.
	if record, ok := value.(archive.CanonicalLogicalExportRecord); ok && len(record.Envelope) != 0 {
		envelope := record.Envelope
		record.Envelope = nil

		skeleton, err := json.Marshal(record)
		if err != nil {
			return invalid("invalid canonical logical record")
		}

		if len(envelope) > MaxEnvelopeBytes ||
			len(skeleton)-2+base64.StdEncoding.EncodedLen(len(envelope))+1 > archive.MaxLogicalLineBytes {
			return limitError()
		}
	}

	data, err := json.Marshal(value)
	if err != nil {
		return invalid("invalid canonical logical JSON")
	}

	if len(data)+1 > archive.MaxLogicalLineBytes {
		return limitError()
	}

	for _, part := range [][]byte{data, {'\n'}} {
		if err := ctx.Err(); err != nil {
			return err
		}

		n, err := destination.Write(part)
		if err != nil {
			return err
		}

		if n != len(part) {
			return io.ErrShortWrite
		}
	}

	return ctx.Err()
}

func logicalBoundaryError(ctx context.Context, err error) error {
	if ctxErr := checkArchiveContext(ctx); ctxErr != nil {
		return ctxErr
	}

	if _, _, classified := memory.ClassifyError(err); err != nil && !classified {
		return memory.RetryableError(memory.CodeStoreFailure, "canonical SQL logical stream failed", nil)
	}

	return err
}

// ImportCanonicalLogical uses the same durable restore authority as opaque SQL
// restore; successful scan callbacks alone never authorize imported state.
func (o *Owner) ImportCanonicalLogical(ctx context.Context, source io.Reader) error {
	_, err := o.ImportCanonicalLogicalWithOptions(ctx, source, archive.CanonicalLogicalImportOptions{})

	return err
}

func (o *Owner) ImportCanonicalLogicalWithOptions(ctx context.Context, source io.Reader,
	options archive.CanonicalLogicalImportOptions,
) (archive.CanonicalLogicalExportSummary, error) {
	if err := checkArchiveContext(ctx); err != nil {
		return archive.CanonicalLogicalExportSummary{}, err
	}

	batch, err := logicalBatchSize(options.BatchSize)
	if err != nil {
		return archive.CanonicalLogicalExportSummary{}, err
	}

	var summary archive.CanonicalLogicalExportSummary

	stage := func(ctx context.Context, source io.Reader, postgres bool) (
		file *os.File, header archive.BackupHeader, digest string, result error,
	) {
		file, err := privateRestoreSpool()
		if err != nil {
			return nil, header, "", err
		}
		defer func() {
			if result != nil {
				_ = file.Close()
			}
		}()

		header, summary, err = scanLogical(ctx, source, batch, nil, file, &postgres)
		if err != nil {
			return file, header, "", err
		}

		_, err = file.Seek(0, io.SeekStart)

		return file, header, summary.SHA256, err
	}
	if err := o.restoreCanonical(ctx, source, batch, stage, options.Progress); err != nil {
		return archive.CanonicalLogicalExportSummary{}, err
	}

	return summary, nil
}

// ScanCanonicalLogicalExport validates without owner admission or state writes.
func (o *Owner) ScanCanonicalLogicalExport(ctx context.Context, source io.Reader, batch int,
	onBatch func([]archive.CanonicalLogicalExportRecord) error,
) (archive.CanonicalLogicalExportSummary, error) {
	return ScanCanonicalLogicalExport(ctx, source, batch, onBatch)
}

func logicalBatchSize(batch int) (int, error) {
	if batch <= 0 {
		batch = 256
	}

	if batch > maxRestoreBatch {
		return 0, limitError()
	}

	return batch, nil
}
