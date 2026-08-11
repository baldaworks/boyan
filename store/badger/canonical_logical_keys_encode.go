package badger

import memory "github.com/baldaworks/boyan/pkg/memory"

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
