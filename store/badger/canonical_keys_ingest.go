package badger

import (
	"encoding/binary"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func badgerIngestSequenceKey(scope memory.Scope, producerID string, sequence uint64) ([]byte, error) {
	prefix, err := badgerIngestSequencePrefix(scope, producerID)
	if err != nil {
		return nil, err
	}
	key := make([]byte, len(prefix)+8)
	copy(key, prefix)
	binary.BigEndian.PutUint64(key[len(prefix):], sequence)
	return key, nil
}

func badgerIngestSequencePrefix(scope memory.Scope, producerID string) ([]byte, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if !isCanonicalProducerID(producerID) {
		return nil, memory.PermanentError(memory.CodeInvalidDerived, "ingest producer id is invalid", nil)
	}
	return memory.TupleKey(canonicalNamespace, canonicalVersion, badgerRecordIngestSequence, scope.Key, string(scope.Kind), producerID)
}

func badgerIngestCursorKey(scope memory.Scope, producerID string) ([]byte, error) {
	if !isCanonicalProducerID(producerID) {
		return nil, memory.PermanentError(memory.CodeInvalidDerived, "ingest producer id is invalid", nil)
	}
	return canonicalKey(scope, badgerRecordIngestCursor, producerID)
}
