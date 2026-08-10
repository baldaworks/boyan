package badger

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// ExportCanonicalLogical writes a deterministic, backend-neutral JSONL
// export. It excludes disposable projection-generation records and includes
// payload blobs as opaque validated envelopes.
func (s *CanonicalStore) ExportCanonicalLogical(ctx context.Context, destination io.Writer) error {
	if err := validateCanonicalMaintenance(ctx, destination); err != nil {
		return err
	}
	if s == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.Lock()
	defer s.maintenanceMu.Unlock()
	if s.db == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	writer := bufio.NewWriter(destination)
	if err := writeCanonicalLogicalJSON(writer, CanonicalLogicalExportHeader{
		SchemaVersion: canonicalLogicalExportSchema,
		Kind:          canonicalLogicalHeaderKind,
	}); err != nil {
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
			decoded, err := memory.UnmarshalRecordEnvelope(envelope)
			if err != nil || decoded.RecordType != components[0] {
				return memory.PermanentError(memory.CodeStoreFailure, "canonical logical export found an invalid record", err)
			}
			record := CanonicalLogicalExportRecord{
				Kind:       canonicalLogicalRecordKind,
				RecordType: components[0],
				Components: components,
				Suffix:     suffix,
				Envelope:   envelope,
			}
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
		return canonicalStoreError("export canonical logical records", err)
	}
	footer := CanonicalLogicalExportFooter{
		SchemaVersion: canonicalLogicalExportSchema,
		Kind:          canonicalLogicalFooterKind,
		RecordCount:   count,
		SHA256:        hex.EncodeToString(hash.Sum(nil)),
	}
	if err := writeCanonicalLogicalJSON(writer, footer); err != nil {
		return err
	}
	return writer.Flush()
}
