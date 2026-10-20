package badger

import (
	"encoding/binary"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func decodeCanonicalTupleKey(key []byte) ([]string, []byte, error) {
	if len(key) < 2 || key[0] != canonicalNamespace || key[1] != canonicalVersion {
		return nil, nil, memory.PermanentError(memory.CodeStoreFailure, "canonical key namespace is invalid", nil)
	}

	components, offset, err := decodeCanonicalTupleComponents(key[2:])
	if err != nil {
		return nil, nil, err
	}

	recordType := components[0]

	want, _ := canonicalTupleComponentCount(recordType)
	if len(components) != want {
		return nil, nil, memory.PermanentError(memory.CodeStoreFailure, "canonical key component count is invalid", nil)
	}

	suffix := key[2+offset:]
	if recordType == badgerRecordChange {
		if len(suffix) != canonicalSequenceBytes {
			return nil, nil, memory.PermanentError(memory.CodeStoreFailure, "canonical change key suffix is invalid", nil)
		}
	} else if len(suffix) != 0 {
		return nil, nil, memory.PermanentError(memory.CodeStoreFailure, "canonical key has an unexpected suffix", nil)
	}

	return components, append([]byte(nil), suffix...), nil
}

func decodeCanonicalTupleComponents(encoded []byte) ([]string, int, error) {
	components := make([]string, 0, canonicalEdgeComponents)

	offset := 0
	for offset+canonicalTupleLengthBytes <= len(encoded) && len(components) < canonicalEdgeComponents {
		length := int(binary.BigEndian.Uint16(encoded[offset : offset+canonicalTupleLengthBytes]))
		if length > len(encoded)-offset-canonicalTupleLengthBytes {
			break
		}

		start := offset + canonicalTupleLengthBytes
		components = append(components, string(encoded[start:start+length]))
		offset = start + length

		if len(components) > 0 {
			if want, _ := canonicalTupleComponentCount(components[0]); want > 0 && len(components) == want {
				return components, offset, nil
			}
		}
	}

	return nil, 0, memory.PermanentError(memory.CodeStoreFailure, "canonical tuple key is malformed", nil)
}
