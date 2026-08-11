package badger

import memory "github.com/baldaworks/boyan/pkg/memory"

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
