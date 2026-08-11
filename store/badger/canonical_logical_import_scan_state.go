package badger

import (
	"crypto/sha256"
	"hash"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

type canonicalLogicalImportScanState struct {
	hash    hash.Hash
	scopes  map[string]memory.Scope
	batches canonicalLogicalImportBatcher
	count   uint64
}

func newCanonicalLogicalImportScanState(batchSize int, onBatch func([]CanonicalLogicalExportRecord) error) canonicalLogicalImportScanState {
	return canonicalLogicalImportScanState{
		hash:    sha256.New(),
		scopes:  make(map[string]memory.Scope),
		batches: newCanonicalLogicalImportBatcher(batchSize, onBatch),
	}
}

func (s *canonicalLogicalImportScanState) addRecord(scan *canonicalLogicalImportScanner, line []byte) error {
	var record CanonicalLogicalExportRecord
	if err := scan.unmarshal(line, &record); err != nil {
		return err
	}
	if err := validateCanonicalLogicalRecord(record); err != nil {
		return err
	}
	if scope, ok := canonicalLogicalScopeRecord(record); ok {
		s.scopes[scope.Key+"\x00"+string(scope.Kind)] = scope
	}
	canonicalLine, err := marshalCanonicalLogicalJSON(record)
	if err != nil {
		return err
	}
	canonicalLine = append(canonicalLine, '\n')
	_, _ = s.hash.Write(canonicalLine)
	if err := s.batches.add(record); err != nil {
		return err
	}
	s.count++
	return nil
}
