package badger

import memory "github.com/baldaworks/boyan/pkg/memory"

func badgerProvenanceKey(scope memory.Scope, recordType, revisionID, relatedRevisionID string) ([]byte, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}

	return memory.TupleKey(
		canonicalNamespace,
		canonicalVersion,
		recordType,
		scope.Key,
		string(scope.Kind),
		revisionID,
		relatedRevisionID,
	)
}

func badgerProvenancePrefix(scope memory.Scope, recordType, revisionID string) ([]byte, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}

	return memory.TupleKey(
		canonicalNamespace,
		canonicalVersion,
		recordType,
		scope.Key,
		string(scope.Kind),
		revisionID,
	)
}
