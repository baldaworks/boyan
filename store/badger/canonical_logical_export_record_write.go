package badger

import (
	"bufio"
	"fmt"
	"hash"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func writeCanonicalLogicalExportRecord(writer *bufio.Writer, digest hash.Hash, item *badger.Item) (bool, error) {
	record, exportable, err := canonicalLogicalExportRecord(item)
	if err != nil || !exportable {
		return exportable, err
	}
	line, err := marshalCanonicalLogicalJSON(record)
	if err != nil {
		return false, err
	}
	line = append(line, '\n')
	if _, err := writer.Write(line); err != nil {
		return false, err
	}
	_, _ = digest.Write(line)
	return true, nil
}

func canonicalLogicalExportRecord(item *badger.Item) (CanonicalLogicalExportRecord, bool, error) {
	components, suffix, err := decodeCanonicalLogicalKey(item.Key())
	if err != nil {
		return CanonicalLogicalExportRecord{}, false, fmt.Errorf("decode canonical key %x: %w", item.Key(), err)
	}
	if !isExportableCanonicalRecordType(components[0]) {
		return CanonicalLogicalExportRecord{}, false, nil
	}
	envelope, err := item.ValueCopy(nil)
	if err != nil {
		return CanonicalLogicalExportRecord{}, false, err
	}
	decoded, err := memory.UnmarshalRecordEnvelope(envelope)
	if err != nil || decoded.RecordType != components[0] {
		return CanonicalLogicalExportRecord{}, false, memory.PermanentError(memory.CodeStoreFailure, "canonical logical export found an invalid record", err)
	}
	return CanonicalLogicalExportRecord{
		Kind:       canonicalLogicalRecordKind,
		RecordType: components[0],
		Components: components,
		Suffix:     suffix,
		Envelope:   envelope,
	}, true, nil
}
