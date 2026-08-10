package badger

import memory "github.com/baldaworks/boyan/pkg/memory"

func isExportableCanonicalRecordType(recordType string) bool {
	return recordType != badgerRecordProjectionManifest &&
		recordType != badgerRecordProjectionActive &&
		recordType != badgerRecordProjectionRetention &&
		recordType != badgerRecordProjectionCheckpoint
}

func decodeCanonicalLogicalKey(key []byte) ([]string, []byte, error) {
	if len(key) < 2 || key[0] != canonicalNamespace || key[1] != canonicalVersion {
		return nil, nil, memory.PermanentError(memory.CodeStoreFailure, "canonical key namespace is invalid", nil)
	}
	components, offset, err := decodeCanonicalTupleComponents(key[2:])
	if err != nil {
		return nil, nil, err
	}
	recordType := components[0]
	want, hasSuffix := canonicalLogicalComponentCount(recordType)
	if (!hasSuffix && len(components) != want) || (hasSuffix && len(components) != want) {
		return nil, nil, memory.PermanentError(memory.CodeStoreFailure, "canonical key component count is invalid", nil)
	}
	suffix := key[2+offset:]
	if recordType == badgerRecordChange {
		if len(suffix) != 8 {
			return nil, nil, memory.PermanentError(memory.CodeStoreFailure, "canonical change key suffix is invalid", nil)
		}
	} else if len(suffix) != 0 {
		return nil, nil, memory.PermanentError(memory.CodeStoreFailure, "canonical key has an unexpected suffix", nil)
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
	return nil, 0, memory.PermanentError(memory.CodeStoreFailure, "canonical tuple key is malformed", nil)
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
		return nil, memory.PermanentError(memory.CodeInvalidDerived, "canonical logical key record type is invalid", nil)
	}
	want, hasSuffix := canonicalLogicalComponentCount(components[0])
	if len(components) != want || (hasSuffix && len(suffix) != 8) || (!hasSuffix && len(suffix) != 0) {
		return nil, memory.PermanentError(memory.CodeInvalidDerived, "canonical logical key components are invalid", nil)
	}
	key, err := memory.TupleKey(canonicalNamespace, canonicalVersion, components...)
	if err != nil {
		return nil, err
	}
	return append(key, suffix...), nil
}
