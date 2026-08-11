package badger

import (
	"encoding/binary"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func badgerScopeChangePrefix(scope memory.Scope) ([]byte, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return memory.TupleKey(canonicalNamespace, canonicalVersion, badgerRecordChange, scope.Key, string(scope.Kind))
}

func badgerScopeChangeKey(scope memory.Scope, sequence uint64) ([]byte, error) {
	prefix, err := badgerScopeChangePrefix(scope)
	if err != nil {
		return nil, err
	}
	key := make([]byte, len(prefix)+8)
	copy(key, prefix)
	binary.BigEndian.PutUint64(key[len(prefix):], sequence)
	return key, nil
}
