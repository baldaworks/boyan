package badger

import memory "github.com/baldaworks/boyan/pkg/memory"

func badgerProjectionManifestKey(scope memory.Scope, projectionID, generationID string) ([]byte, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}

	return memory.TupleKey(
		canonicalNamespace,
		canonicalVersion,
		badgerRecordProjectionManifest,
		scope.Key,
		string(scope.Kind),
		projectionID,
		generationID,
	)
}

func badgerProjectionRetentionKey(scope memory.Scope, projectionID string) ([]byte, error) {
	return canonicalKey(scope, badgerRecordProjectionRetention, projectionID)
}

func badgerProjectionCheckpointKey(scope memory.Scope, kind memory.ScopeCheckpointKind) ([]byte, error) {
	return canonicalKey(scope, badgerRecordProjectionCheckpoint, string(kind))
}
