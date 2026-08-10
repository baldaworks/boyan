package badger

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// ImportCanonicalLogical validates a logical export stream while applying it
// in bounded, idempotent transactions. A malformed footer still fails closed;
// callers that need restart-safe resume should persist external progress.
func (s *CanonicalStore) ImportCanonicalLogical(ctx context.Context, source io.Reader) error {
	_, err := s.ImportCanonicalLogicalWithOptions(ctx, source, CanonicalLogicalImportOptions{})
	return err
}

// ImportCanonicalLogicalWithOptions applies a validated logical export through
// bounded transactions and reports the validated stream summary.
func (s *CanonicalStore) ImportCanonicalLogicalWithOptions(ctx context.Context, source io.Reader, options CanonicalLogicalImportOptions) (CanonicalLogicalExportSummary, error) {
	if err := validateCanonicalMaintenance(ctx, source); err != nil {
		return CanonicalLogicalExportSummary{}, err
	}
	if s == nil {
		return CanonicalLogicalExportSummary{}, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	batchSize := options.BatchSize
	if batchSize <= 0 {
		batchSize = defaultCanonicalImportBatch
	}
	if batchSize > 4096 {
		return CanonicalLogicalExportSummary{}, memory.PermanentError(memory.CodeLimitExceeded, "canonical logical import batch size is too large", nil)
	}
	s.maintenanceMu.Lock()
	defer s.maintenanceMu.Unlock()
	if s.db == nil {
		return CanonicalLogicalExportSummary{}, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	var imported uint64
	summary, err := ScanCanonicalLogicalExport(ctx, source, batchSize, func(records []CanonicalLogicalExportRecord) error {
		err := s.db.Update(func(txn *badger.Txn) error {
			return applyCanonicalLogicalImportBatch(txn, records)
		})
		if err != nil {
			return canonicalStoreError("import canonical logical records", err)
		}
		imported += uint64(len(records))
		if options.Progress != nil {
			if err := options.Progress(CanonicalLogicalImportProgress{ImportedRecords: imported}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return CanonicalLogicalExportSummary{}, err
	}
	return summary, nil
}

// ScanCanonicalLogicalExport validates a full logical export stream, reports
// its footer metadata, and optionally processes validated records in batches.
func ScanCanonicalLogicalExport(ctx context.Context, source io.Reader, batchSize int, onBatch func([]CanonicalLogicalExportRecord) error) (CanonicalLogicalExportSummary, error) {
	if err := validateCanonicalMaintenance(ctx, source); err != nil {
		return CanonicalLogicalExportSummary{}, err
	}
	if batchSize <= 0 {
		batchSize = defaultCanonicalImportBatch
	}
	if batchSize > 4096 {
		return CanonicalLogicalExportSummary{}, memory.PermanentError(memory.CodeLimitExceeded, "canonical logical batch size is too large", nil)
	}
	scanner := bufio.NewScanner(source)
	scanner.Buffer(make([]byte, 64*1024), maxCanonicalLogicalLineBytes)
	lineNumber := 0
	readLine := func(target any) error {
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return err
			}
			return io.ErrUnexpectedEOF
		}
		lineNumber++
		if err := json.Unmarshal(scanner.Bytes(), target); err != nil {
			return fmt.Errorf("decode canonical logical export line %d: %w", lineNumber, err)
		}
		return nil
	}
	var header CanonicalLogicalExportHeader
	if err := readLine(&header); err != nil {
		return CanonicalLogicalExportSummary{}, err
	}
	if header.SchemaVersion != canonicalLogicalExportSchema || header.Kind != canonicalLogicalHeaderKind {
		return CanonicalLogicalExportSummary{}, memory.PermanentError(memory.CodeInvalidDerived, "canonical logical export header is invalid", nil)
	}
	hash := sha256.New()
	scopes := make(map[string]memory.Scope)
	records := make([]CanonicalLogicalExportRecord, 0, batchSize)
	flush := func() error {
		if len(records) == 0 || onBatch == nil {
			return nil
		}
		batch := append([]CanonicalLogicalExportRecord(nil), records...)
		if err := onBatch(batch); err != nil {
			return err
		}
		return nil
	}
	var count uint64
	for {
		if err := checkCanonicalMaintenanceContext(ctx); err != nil {
			return CanonicalLogicalExportSummary{}, err
		}
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return CanonicalLogicalExportSummary{}, err
			}
			return CanonicalLogicalExportSummary{}, io.ErrUnexpectedEOF
		}
		lineNumber++
		line := append([]byte(nil), scanner.Bytes()...)
		var kind struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(line, &kind); err != nil {
			return CanonicalLogicalExportSummary{}, fmt.Errorf("decode canonical logical export line %d: %w", lineNumber, err)
		}
		switch kind.Kind {
		case canonicalLogicalRecordKind:
			var record CanonicalLogicalExportRecord
			if err := json.Unmarshal(line, &record); err != nil {
				return CanonicalLogicalExportSummary{}, fmt.Errorf("decode canonical logical record line %d: %w", lineNumber, err)
			}
			if err := validateCanonicalLogicalRecord(record); err != nil {
				return CanonicalLogicalExportSummary{}, err
			}
			if scope, ok := canonicalLogicalScopeRecord(record); ok {
				scopes[scope.Key+"\x00"+string(scope.Kind)] = scope
			}
			canonicalLine, err := marshalCanonicalLogicalJSON(record)
			if err != nil {
				return CanonicalLogicalExportSummary{}, err
			}
			canonicalLine = append(canonicalLine, '\n')
			_, _ = hash.Write(canonicalLine)
			records = append(records, record)
			count++
			if len(records) == batchSize {
				if err := flush(); err != nil {
					return CanonicalLogicalExportSummary{}, err
				}
				records = records[:0]
			}
		case canonicalLogicalFooterKind:
			var footer CanonicalLogicalExportFooter
			if err := json.Unmarshal(line, &footer); err != nil {
				return CanonicalLogicalExportSummary{}, fmt.Errorf("decode canonical logical footer line %d: %w", lineNumber, err)
			}
			if footer.SchemaVersion != canonicalLogicalExportSchema || footer.Kind != canonicalLogicalFooterKind || footer.RecordCount != count || footer.SHA256 != hex.EncodeToString(hash.Sum(nil)) {
				return CanonicalLogicalExportSummary{}, memory.PermanentError(memory.CodeInvalidDerived, "canonical logical export footer does not match records", nil)
			}
			if err := flush(); err != nil {
				return CanonicalLogicalExportSummary{}, err
			}
			if scanner.Scan() {
				return CanonicalLogicalExportSummary{}, memory.PermanentError(memory.CodeInvalidDerived, "canonical logical export has trailing records", nil)
			}
			if err := scanner.Err(); err != nil {
				return CanonicalLogicalExportSummary{}, err
			}
			return CanonicalLogicalExportSummary{
				RecordCount: footer.RecordCount,
				SHA256:      footer.SHA256,
				Scopes:      sortedCanonicalLogicalScopes(scopes),
			}, nil
		default:
			return CanonicalLogicalExportSummary{}, memory.PermanentError(memory.CodeInvalidDerived, "canonical logical export record kind is invalid", nil)
		}
	}
}

func applyCanonicalLogicalImportBatch(txn *badger.Txn, records []CanonicalLogicalExportRecord) error {
	for _, record := range records {
		key, err := encodeCanonicalLogicalKey(record.Components, record.Suffix)
		if err != nil {
			return err
		}
		item, err := txn.Get(key)
		if err == nil {
			existing, copyErr := item.ValueCopy(nil)
			if copyErr != nil {
				return copyErr
			}
			if !bytes.Equal(existing, record.Envelope) {
				return memory.PermanentError(memory.CodeConflict, "canonical logical import collides with existing record", nil)
			}
			continue
		}
		if !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}
		if err := txn.Set(key, record.Envelope); err != nil {
			return err
		}
	}
	return nil
}

func validateCanonicalLogicalRecord(record CanonicalLogicalExportRecord) error {
	if record.Kind != canonicalLogicalRecordKind || record.RecordType == "" || len(record.Envelope) == 0 {
		return memory.PermanentError(memory.CodeInvalidDerived, "canonical logical record is invalid", nil)
	}
	if !isExportableCanonicalRecordType(record.RecordType) || len(record.Components) == 0 || record.Components[0] != record.RecordType {
		return memory.PermanentError(memory.CodeInvalidDerived, "canonical logical record tuple is invalid", nil)
	}
	if _, err := encodeCanonicalLogicalKey(record.Components, record.Suffix); err != nil {
		return err
	}
	envelope, err := memory.UnmarshalRecordEnvelope(record.Envelope)
	if err != nil || envelope.RecordType != record.RecordType {
		return memory.PermanentError(memory.CodeInvalidDerived, "canonical logical record envelope is invalid", err)
	}
	return nil
}

func canonicalLogicalScopeRecord(record CanonicalLogicalExportRecord) (memory.Scope, bool) {
	if record.RecordType != badgerRecordScope || len(record.Components) != 3 {
		return memory.Scope{}, false
	}
	scope := memory.Scope{Key: record.Components[1], Kind: memory.ScopeKind(record.Components[2])}
	if err := scope.Validate(); err != nil {
		return memory.Scope{}, false
	}
	return scope, true
}

func sortedCanonicalLogicalScopes(scopes map[string]memory.Scope) []memory.Scope {
	result := make([]memory.Scope, 0, len(scopes))
	for _, scope := range scopes {
		result = append(result, scope)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Key != result[j].Key {
			return result[i].Key < result[j].Key
		}
		return result[i].Kind < result[j].Kind
	})
	return result
}

func writeCanonicalLogicalJSON(writer io.Writer, value any) error {
	line, err := marshalCanonicalLogicalJSON(value)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	_, err = writer.Write(line)
	return err
}

func marshalCanonicalLogicalJSON(value any) ([]byte, error) {
	line, err := json.Marshal(value)
	if err != nil {
		return nil, memory.PermanentError(memory.CodeInvalidDerived, "encode canonical logical export record", err)
	}
	return line, nil
}
