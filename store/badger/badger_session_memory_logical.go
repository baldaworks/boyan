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

	"github.com/dgraph-io/badger/v4"
	"github.com/baldaworks/boyan"
)

const (
	canonicalLogicalExportSchema = "session-memory-logical-export/v1"
	canonicalLogicalHeaderKind   = "header"
	canonicalLogicalRecordKind   = "record"
	canonicalLogicalFooterKind   = "footer"
	maxCanonicalLogicalLineBytes = 4 * 1024 * 1024
	defaultCanonicalImportBatch  = 256
)

// CanonicalLogicalExportHeader identifies the application-owned JSONL format.
// Backend-native Badger keys, versions, and projection files are not exposed.
type CanonicalLogicalExportHeader struct {
	SchemaVersion string `json:"schema_version"`
	Kind          string `json:"kind"`
}

// CanonicalLogicalExportRecord is one portable tuple-key record. Envelope is
// retained as opaque validated bytes so storage encoding stays independent of
// the logical payload representation during export/import.
type CanonicalLogicalExportRecord struct {
	Kind       string   `json:"kind"`
	RecordType string   `json:"record_type"`
	Components []string `json:"components"`
	Suffix     []byte   `json:"suffix,omitempty"`
	Envelope   []byte   `json:"envelope"`
}

// CanonicalLogicalExportFooter authenticates the record stream and makes
// truncation detectable before import writes anything.
type CanonicalLogicalExportFooter struct {
	SchemaVersion string `json:"schema_version"`
	Kind          string `json:"kind"`
	RecordCount   uint64 `json:"record_count"`
	SHA256        string `json:"sha256"`
}

// CanonicalLogicalExportSummary reports validated stream metadata without
// exposing backend-native record layout. Scopes come only from canonical scope
// state records, so internal payload storage is excluded from rebuild inputs.
type CanonicalLogicalExportSummary struct {
	RecordCount uint64
	SHA256      string
	Scopes      []boyan.Scope
}

// CanonicalLogicalImportOptions controls bounded, resumable import batches.
type CanonicalLogicalImportOptions struct {
	BatchSize int
	Progress  func(CanonicalLogicalImportProgress) error
}

// CanonicalLogicalImportProgress reports durable batch progress after one
// transaction committed.
type CanonicalLogicalImportProgress struct {
	ImportedRecords uint64
}

// ExportCanonicalLogical writes a deterministic, backend-neutral JSONL
// export. It excludes disposable projection-generation records and includes
// payload blobs as opaque validated envelopes.
func (s *BadgerSessionMemoryStore) ExportCanonicalLogical(ctx context.Context, destination io.Writer) error {
	if err := validateCanonicalMaintenance(ctx, destination); err != nil {
		return err
	}
	if s == nil {
		return boyan.PermanentError(boyan.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.Lock()
	defer s.maintenanceMu.Unlock()
	if s.db == nil {
		return boyan.PermanentError(boyan.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	writer := bufio.NewWriter(destination)
	if err := writeCanonicalLogicalJSON(writer, CanonicalLogicalExportHeader{SchemaVersion: canonicalLogicalExportSchema, Kind: canonicalLogicalHeaderKind}); err != nil {
		return err
	}
	hash := sha256.New()
	var count uint64
	err := s.db.View(func(txn *badger.Txn) error {
		iterator := txn.NewIterator(badger.DefaultIteratorOptions)
		defer iterator.Close()
		for iterator.Rewind(); iterator.Valid(); iterator.Next() {
			if err := checkCanonicalMaintenanceContext(ctx); err != nil {
				return err
			}
			components, suffix, err := decodeCanonicalLogicalKey(iterator.Item().Key())
			if err != nil {
				return fmt.Errorf("decode canonical key %x: %w", iterator.Item().Key(), err)
			}
			if !isExportableCanonicalRecordType(components[0]) {
				continue
			}
			envelope, err := iterator.Item().ValueCopy(nil)
			if err != nil {
				return err
			}
			decoded, err := boyan.UnmarshalRecordEnvelope(envelope)
			if err != nil || decoded.RecordType != components[0] {
				return boyan.PermanentError(boyan.CodeStoreFailure, "canonical logical export found an invalid record", err)
			}
			record := CanonicalLogicalExportRecord{Kind: canonicalLogicalRecordKind, RecordType: components[0], Components: components, Suffix: suffix, Envelope: envelope}
			line, err := marshalCanonicalLogicalJSON(record)
			if err != nil {
				return err
			}
			line = append(line, '\n')
			if _, err := writer.Write(line); err != nil {
				return err
			}
			_, _ = hash.Write(line)
			count++
		}
		return nil
	})
	if err != nil {
		return badgerSessionMemoryError("export canonical logical records", err)
	}
	footer := CanonicalLogicalExportFooter{SchemaVersion: canonicalLogicalExportSchema, Kind: canonicalLogicalFooterKind, RecordCount: count, SHA256: hex.EncodeToString(hash.Sum(nil))}
	if err := writeCanonicalLogicalJSON(writer, footer); err != nil {
		return err
	}
	return writer.Flush()
}

// ImportCanonicalLogical validates a logical export stream while applying it
// in bounded, idempotent transactions. A malformed footer still fails closed;
// callers that need restart-safe resume should persist external progress.
func (s *BadgerSessionMemoryStore) ImportCanonicalLogical(ctx context.Context, source io.Reader) error {
	_, err := s.ImportCanonicalLogicalWithOptions(ctx, source, CanonicalLogicalImportOptions{})
	return err
}

// ImportCanonicalLogicalWithOptions applies a validated logical export through
// bounded transactions and reports the validated stream summary.
func (s *BadgerSessionMemoryStore) ImportCanonicalLogicalWithOptions(ctx context.Context, source io.Reader, options CanonicalLogicalImportOptions) (CanonicalLogicalExportSummary, error) {
	if err := validateCanonicalMaintenance(ctx, source); err != nil {
		return CanonicalLogicalExportSummary{}, err
	}
	if s == nil {
		return CanonicalLogicalExportSummary{}, boyan.PermanentError(boyan.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	batchSize := options.BatchSize
	if batchSize <= 0 {
		batchSize = defaultCanonicalImportBatch
	}
	if batchSize > 4096 {
		return CanonicalLogicalExportSummary{}, boyan.PermanentError(boyan.CodeLimitExceeded, "canonical logical import batch size is too large", nil)
	}
	s.maintenanceMu.Lock()
	defer s.maintenanceMu.Unlock()
	if s.db == nil {
		return CanonicalLogicalExportSummary{}, boyan.PermanentError(boyan.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	var imported uint64
	summary, err := ScanCanonicalLogicalExport(ctx, source, batchSize, func(records []CanonicalLogicalExportRecord) error {
		err := s.db.Update(func(txn *badger.Txn) error {
			return applyCanonicalLogicalImportBatch(txn, records)
		})
		if err != nil {
			return badgerSessionMemoryError("import canonical logical records", err)
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
		return CanonicalLogicalExportSummary{}, boyan.PermanentError(boyan.CodeLimitExceeded, "canonical logical batch size is too large", nil)
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
		return CanonicalLogicalExportSummary{}, boyan.PermanentError(boyan.CodeInvalidDerived, "canonical logical export header is invalid", nil)
	}
	hash := sha256.New()
	scopes := make(map[string]boyan.Scope)
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
				return CanonicalLogicalExportSummary{}, boyan.PermanentError(boyan.CodeInvalidDerived, "canonical logical export footer does not match records", nil)
			}
			if err := flush(); err != nil {
				return CanonicalLogicalExportSummary{}, err
			}
			if scanner.Scan() {
				return CanonicalLogicalExportSummary{}, boyan.PermanentError(boyan.CodeInvalidDerived, "canonical logical export has trailing records", nil)
			}
			if err := scanner.Err(); err != nil {
				return CanonicalLogicalExportSummary{}, err
			}
			return CanonicalLogicalExportSummary{RecordCount: footer.RecordCount, SHA256: footer.SHA256, Scopes: sortedCanonicalLogicalScopes(scopes)}, nil
		default:
			return CanonicalLogicalExportSummary{}, boyan.PermanentError(boyan.CodeInvalidDerived, "canonical logical export record kind is invalid", nil)
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
				return boyan.PermanentError(boyan.CodeConflict, "canonical logical import collides with existing record", nil)
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
		return boyan.PermanentError(boyan.CodeInvalidDerived, "canonical logical record is invalid", nil)
	}
	if !isExportableCanonicalRecordType(record.RecordType) || len(record.Components) == 0 || record.Components[0] != record.RecordType {
		return boyan.PermanentError(boyan.CodeInvalidDerived, "canonical logical record tuple is invalid", nil)
	}
	if _, err := encodeCanonicalLogicalKey(record.Components, record.Suffix); err != nil {
		return err
	}
	envelope, err := boyan.UnmarshalRecordEnvelope(record.Envelope)
	if err != nil || envelope.RecordType != record.RecordType {
		return boyan.PermanentError(boyan.CodeInvalidDerived, "canonical logical record envelope is invalid", err)
	}
	return nil
}

func canonicalLogicalScopeRecord(record CanonicalLogicalExportRecord) (boyan.Scope, bool) {
	if record.RecordType != badgerRecordScope || len(record.Components) != 3 {
		return boyan.Scope{}, false
	}
	scope := boyan.Scope{Key: record.Components[1], Kind: boyan.ScopeKind(record.Components[2])}
	if err := scope.Validate(); err != nil {
		return boyan.Scope{}, false
	}
	return scope, true
}

func sortedCanonicalLogicalScopes(scopes map[string]boyan.Scope) []boyan.Scope {
	result := make([]boyan.Scope, 0, len(scopes))
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
		return nil, boyan.PermanentError(boyan.CodeInvalidDerived, "encode canonical logical export record", err)
	}
	return line, nil
}

func isExportableCanonicalRecordType(recordType string) bool {
	return recordType != badgerRecordProjectionManifest && recordType != badgerRecordProjectionActive &&
		recordType != badgerRecordProjectionRetention && recordType != badgerRecordProjectionCheckpoint
}

func decodeCanonicalLogicalKey(key []byte) ([]string, []byte, error) {
	if len(key) < 2 || key[0] != badgerSessionMemoryNamespace || key[1] != badgerSessionMemoryVersion {
		return nil, nil, boyan.PermanentError(boyan.CodeStoreFailure, "canonical key namespace is invalid", nil)
	}
	components, offset, err := decodeCanonicalTupleComponents(key[2:])
	if err != nil {
		return nil, nil, err
	}
	recordType := components[0]
	want, hasSuffix := canonicalLogicalComponentCount(recordType)
	if !hasSuffix && len(components) != want || hasSuffix && len(components) != want {
		return nil, nil, boyan.PermanentError(boyan.CodeStoreFailure, "canonical key component count is invalid", nil)
	}
	suffix := key[2+offset:]
	if recordType == badgerRecordChange {
		if len(suffix) != 8 {
			return nil, nil, boyan.PermanentError(boyan.CodeStoreFailure, "canonical change key suffix is invalid", nil)
		}
	} else if len(suffix) != 0 {
		return nil, nil, boyan.PermanentError(boyan.CodeStoreFailure, "canonical key has an unexpected suffix", nil)
	}
	return components, append([]byte(nil), suffix...), nil
}

func decodeCanonicalTupleComponents(encoded []byte) ([]string, int, error) {
	components := make([]string, 0, 5)
	offset := 0
	for offset+2 <= len(encoded) && len(components) < 5 {
		length := int(encoded[offset])<<8 | int(encoded[offset+1])
		if length > len(encoded)-offset-2 {
			break
		}
		start := offset + 2
		components = append(components, string(encoded[start:start+length]))
		offset = start + length
		if len(components) > 0 {
			if want, _ := canonicalLogicalComponentCount(components[0]); want > 0 && len(components) == want {
				return components, offset, nil
			}
		}
	}
	return nil, 0, boyan.PermanentError(boyan.CodeStoreFailure, "canonical tuple key is malformed", nil)
}

func canonicalLogicalComponentCount(recordType string) (int, bool) {
	switch recordType {
	case badgerRecordScope, badgerRecordChange:
		return 3, recordType == badgerRecordChange
	case badgerRecordProvenanceForward, badgerRecordProvenanceReverse, badgerRecordSourceRevision, badgerRecordProjectionManifest:
		return 5, false
	case badgerRecordOperation, badgerRecordSource, badgerRecordMessage, badgerRecordItem, badgerRecordRevision, badgerRecordLifecycle, badgerRecordHead, badgerRecordDelivery, badgerRecordDeliveryClaim, badgerRecordPayload, badgerRecordDeniedSource, badgerRecordDeniedRevision, badgerRecordProjectionActive, badgerRecordProjectionRetention, badgerRecordProjectionCheckpoint:
		return 4, false
	default:
		return 0, false
	}
}

func encodeCanonicalLogicalKey(components []string, suffix []byte) ([]byte, error) {
	if len(components) == 0 || !isExportableCanonicalRecordType(components[0]) {
		return nil, boyan.PermanentError(boyan.CodeInvalidDerived, "canonical logical key record type is invalid", nil)
	}
	want, hasSuffix := canonicalLogicalComponentCount(components[0])
	if len(components) != want || (hasSuffix && len(suffix) != 8) || (!hasSuffix && len(suffix) != 0) {
		return nil, boyan.PermanentError(boyan.CodeInvalidDerived, "canonical logical key components are invalid", nil)
	}
	key, err := boyan.TupleKey(badgerSessionMemoryNamespace, badgerSessionMemoryVersion, components...)
	if err != nil {
		return nil, err
	}
	return append(key, suffix...), nil
}
