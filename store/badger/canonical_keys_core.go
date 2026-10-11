package badger

import memory "github.com/baldaworks/boyan/pkg/memory"

func badgerScopeKey(scope memory.Scope) ([]byte, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}

	return memory.TupleKey(
		canonicalNamespace,
		canonicalVersion,
		badgerRecordScope,
		scope.Key,
		string(scope.Kind),
	)
}

func badgerOperationKey(scope memory.Scope, operationID string) ([]byte, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}

	return canonicalKey(scope, badgerRecordOperation, operationID)
}

func badgerForgetOutcomeKey(scope memory.Scope, kind memory.ForgetKind, operationID string) ([]byte, error) {
	return canonicalKey(scope, badgerRecordForgetOutcome, string(kind)+"\x00"+operationID)
}

func canonicalKey(scope memory.Scope, recordType string, id string) ([]byte, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}

	return memory.TupleKey(canonicalNamespace, canonicalVersion, recordType, scope.Key, string(scope.Kind), id)
}

func canonicalPrefix(scope memory.Scope, recordType string) ([]byte, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}

	return memory.TupleKey(canonicalNamespace, canonicalVersion, recordType, scope.Key, string(scope.Kind))
}
