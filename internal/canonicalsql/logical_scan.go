package canonicalsql

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sort"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/archive"
)

// ScanCanonicalLogicalExport is shared by both public SQL package scanners.
func ScanCanonicalLogicalExport(ctx context.Context, source io.Reader, batch int,
	onBatch func([]archive.CanonicalLogicalExportRecord) error,
) (summary archive.CanonicalLogicalExportSummary, result error) {
	if err := checkArchiveContext(ctx); err != nil {
		return summary, err
	}

	if source == nil || nilBackupStream(source) {
		return summary, invalid("nil canonical logical source")
	}

	batch, err := logicalBatchSize(batch)
	if err != nil {
		return summary, err
	}

	defer func() {
		result = logicalBoundaryError(ctx, result)
		if result != nil {
			summary = archive.CanonicalLogicalExportSummary{}
		}
	}()

	_, summary, result = scanLogical(ctx, source, batch, onBatch, nil, nil)

	return summary, result
}

// logicalLine never allocates from an untrusted size. Only fixed-size reader
// fragments are appended after checking the complete line bound, newline included.
func logicalLine(ctx context.Context, reader *bufio.Reader) ([]byte, error) {
	var line []byte

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		part, err := reader.ReadSlice('\n')
		if len(part) > archive.MaxLogicalLineBytes-len(line) {
			return nil, limitError()
		}

		line = append(line, part...)
		if err == nil {
			return line, nil
		}

		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}

		if errors.Is(err, io.EOF) {
			return nil, io.ErrUnexpectedEOF
		}

		return nil, err
	}
}

//nolint:cyclop,gocognit,gocyclo,funlen,nestif // Bounded stream, exact index and callback lifecycle.
func scanLogical(ctx context.Context, source io.Reader, batch int,
	onBatch func([]archive.CanonicalLogicalExportRecord) error, spool io.Writer, postgres *bool,
) (header archive.BackupHeader, summary archive.CanonicalLogicalExportSummary, result error) {
	index, err := privateRestoreSpool()
	if err != nil {
		return header, summary, err
	}
	defer func() { result = errors.Join(result, index.Close()) }()

	keys := restoreKeyIndex{file: index}

	const logicalReaderBytes = 32 * 1024

	reader := bufio.NewReaderSize(source, logicalReaderBytes)

	line, err := logicalLine(ctx, reader)
	if err != nil {
		return header, summary, err
	}

	var wire archive.CanonicalLogicalExportHeader
	if err := decodeRestoreJSON(line, &wire); err != nil {
		return header, summary, err
	}

	header = archive.BackupHeader{
		Kind:            wire.Kind,
		Format:          wire.SchemaVersion,
		SourceStoreUUID: wire.SourceStoreUUID,
		Backend:         wire.Backend,
		SchemaVersion:   wire.StoreSchemaVersion,
		PrivacyProtocol: wire.PrivacyProtocol,
		ProofProtocol:   wire.ProofProtocol,
		Mode:            wire.Mode,
		Base:            wire.Base,
		End:             wire.End,
	}
	if wire.SchemaVersion != archive.SQLLogicalFormat || (header.Backend != "sqlite" && header.Backend != "postgres") {
		return header, summary, invalid("unsupported canonical logical header")
	}

	pg := header.Backend == "postgres"
	if postgres != nil {
		pg = *postgres
	}

	if err := validateRestoreHeader(header, pg); err != nil {
		return header, summary, err
	}

	digest := sha256.New()
	_, _ = digest.Write(line)

	var (
		previous *StampCursor
		records  []archive.CanonicalLogicalExportRecord
		scopes   []memory.Scope
	)

	flush := func() error {
		if onBatch == nil || len(records) == 0 {
			return nil
		}

		ready := records
		records = nil

		return onBatch(ready)
	}

	for {
		line, err := logicalLine(ctx, reader)
		if err != nil {
			return header, summary, err
		}

		var kind struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal(line, &kind) != nil {
			return header, summary, invalid("invalid canonical logical line")
		}

		if kind.Kind == "footer" {
			var footer archive.CanonicalLogicalExportFooter
			if decodeRestoreJSON(line, &footer) != nil || footer.SchemaVersion != archive.SQLLogicalFormat ||
				footer.RecordCount != summary.RecordCount || footer.SHA256 != hex.EncodeToString(digest.Sum(nil)) {
				return header, summary, invalid("invalid canonical logical checksum or count")
			}

			if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
				return header, summary, invalid("trailing canonical logical data")
			}

			if err := flush(); err != nil {
				return header, summary, err
			}

			if err := ctx.Err(); err != nil {
				return header, summary, err
			}

			sort.Slice(scopes, func(i, j int) bool {
				if scopes[i].Key != scopes[j].Key {
					return scopes[i].Key < scopes[j].Key
				}

				return scopes[i].Kind < scopes[j].Kind
			})
			summary.Scopes, summary.SHA256 = scopes, footer.SHA256

			return header, summary, nil
		}

		record, logical, key, data, err := decodeLogicalRecord(line)
		if err != nil {
			return header, summary, err
		}

		_, values, _ := key.values()

		cursor := StampCursor{Sequence: record.Sequence, Family: key.Family, BinaryKey: stampKey(values)}
		if cursor.Sequence == 0 || cursor.Sequence > header.End ||
			(previous != nil && !restoreCursorAfter(cursor, *previous)) {
			return header, summary, invalid("invalid canonical logical record order")
		}

		if err := keys.add(ctx, stampKey(append([][]byte{[]byte(key.Family)}, values...))); err != nil {
			return header, summary, err
		}

		previous = &cursor

		if key.Family == ScopeState && !record.Deleted {
			scopes = append(scopes, key.Scope)
		}

		if spool != nil {
			var prefix [4]byte
			binary.BigEndian.PutUint32(prefix[:], uint32(len(data)))

			for _, part := range [][]byte{prefix[:], data} {
				n, err := spool.Write(part)
				if err != nil {
					return header, summary, err
				}

				if n != len(part) {
					return header, summary, io.ErrShortWrite
				}
			}
		}

		_, _ = digest.Write(line)
		summary.RecordCount++

		if onBatch != nil {
			records = append(records, logical)
			if len(records) == batch {
				if err := flush(); err != nil {
					return header, summary, err
				}
			}
		}
	}
}

func decodeLogicalRecord(line []byte) (archive.BackupRecord, archive.CanonicalLogicalExportRecord, Key, []byte, error) {
	var (
		wire struct {
			Kind       string          `json:"kind"`
			RecordType string          `json:"record_type"`
			Components json.RawMessage `json:"components"`
			Suffix     string          `json:"suffix,omitempty"`
			Envelope   string          `json:"envelope"`
			Sequence   uint64          `json:"sequence,omitempty"`
			Deleted    bool            `json:"deleted,omitempty"`
		}
		record  archive.BackupRecord
		logical archive.CanonicalLogicalExportRecord
	)
	if err := decodeRestoreJSON(line, &wire); err != nil {
		return record, logical, Key{}, nil, err
	}

	if wire.Suffix != "" {
		return record, logical, Key{}, nil, invalid("unsupported canonical logical suffix")
	}
	// Preserve the encoded strings until decodeRestoreRecord has bounded their
	// decoded lengths and validated all tuple/envelope/proof bindings.
	data, err := json.Marshal(struct {
		Kind       string          `json:"kind"`
		Family     string          `json:"family"`
		Components json.RawMessage `json:"components"`
		Sequence   uint64          `json:"sequence"`
		Deleted    bool            `json:"deleted"`
		Envelope   string          `json:"envelope,omitempty"`
	}{wire.Kind, wire.RecordType, wire.Components, wire.Sequence, wire.Deleted, wire.Envelope})
	if err != nil {
		return record, logical, Key{}, nil, invalid("invalid canonical logical record")
	}

	record, key, err := decodeRestoreRecord(data)
	if err != nil {
		return record, logical, key, nil, err
	}

	logical = archive.CanonicalLogicalExportRecord{Kind: record.Kind, RecordType: record.Family,
		Sequence: record.Sequence, Deleted: record.Deleted, Envelope: record.Envelope}
	if err := json.Unmarshal(wire.Components, &logical.Components); err != nil {
		return record, logical, key, nil, invalid("invalid canonical logical components")
	}

	return record, logical, key, data, nil
}
