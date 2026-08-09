package badger

import (
	"encoding/binary"

	"github.com/baldaworks/boyan"
)

const (
	badgerSessionMemoryNamespace     byte = 0x53
	badgerSessionMemoryVersion       byte = 1
	badgerRecordScope                     = "scope"
	badgerRecordOperation                 = "operation"
	badgerRecordSource                    = "source"
	badgerRecordMessage                   = "message"
	badgerRecordItem                      = "item"
	badgerRecordRevision                  = "revision"
	badgerRecordLifecycle                 = "lifecycle"
	badgerRecordHead                      = "head"
	badgerRecordChange                    = "change"
	badgerRecordDelivery                  = "delivery"
	badgerRecordDeliveryClaim             = "delivery-claim"
	badgerRecordPayload                   = "payload"
	badgerRecordProvenanceForward         = "provenance-forward"
	badgerRecordProvenanceReverse         = "provenance-reverse"
	badgerRecordSourceRevision            = "source-revision"
	badgerRecordDeniedSource              = "denied-source"
	badgerRecordDeniedRevision            = "denied-revision"
	badgerRecordProjectionManifest        = "projection-manifest"
	badgerRecordProjectionActive          = "projection-active"
	badgerRecordProjectionRetention       = "projection-retention"
	badgerRecordProjectionCheckpoint      = "projection-checkpoint"
	badgerRecordForgetOutcome             = "forget-outcome"
	badgerRecordIngestSequence            = "ingest-sequence"
	badgerRecordIngestCursor              = "ingest-cursor"
)

func badgerScopeKey(scope boyan.Scope) ([]byte, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return boyan.TupleKey(badgerSessionMemoryNamespace, badgerSessionMemoryVersion, badgerRecordScope, scope.Key, string(scope.Kind))
}

func badgerProvenanceKey(scope boyan.Scope, recordType, revisionID, relatedRevisionID string) ([]byte, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return boyan.TupleKey(badgerSessionMemoryNamespace, badgerSessionMemoryVersion, recordType, scope.Key, string(scope.Kind), revisionID, relatedRevisionID)
}

func badgerProvenancePrefix(scope boyan.Scope, recordType, revisionID string) ([]byte, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return boyan.TupleKey(badgerSessionMemoryNamespace, badgerSessionMemoryVersion, recordType, scope.Key, string(scope.Kind), revisionID)
}

func badgerOperationKey(scope boyan.Scope, operationID string) ([]byte, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return badgerSessionMemoryKey(scope, badgerRecordOperation, operationID)
}

func badgerForgetOutcomeKey(scope boyan.Scope, kind boyan.ForgetKind, operationID string) ([]byte, error) {
	return badgerSessionMemoryKey(scope, badgerRecordForgetOutcome, string(kind)+"\x00"+operationID)
}

func badgerIngestSequenceKey(scope boyan.Scope, producerID string, sequence uint64) ([]byte, error) {
	prefix, err := badgerIngestSequencePrefix(scope, producerID)
	if err != nil {
		return nil, err
	}
	key := make([]byte, len(prefix)+8)
	copy(key, prefix)
	binary.BigEndian.PutUint64(key[len(prefix):], sequence)
	return key, nil
}

func badgerIngestSequencePrefix(scope boyan.Scope, producerID string) ([]byte, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if !isCanonicalProducerID(producerID) {
		return nil, boyan.PermanentError(boyan.CodeInvalidDerived, "ingest producer id is invalid", nil)
	}
	return boyan.TupleKey(badgerSessionMemoryNamespace, badgerSessionMemoryVersion, badgerRecordIngestSequence, scope.Key, string(scope.Kind), producerID)
}

func badgerIngestCursorKey(scope boyan.Scope, producerID string) ([]byte, error) {
	if !isCanonicalProducerID(producerID) {
		return nil, boyan.PermanentError(boyan.CodeInvalidDerived, "ingest producer id is invalid", nil)
	}
	return badgerSessionMemoryKey(scope, badgerRecordIngestCursor, producerID)
}

func badgerSessionMemoryKey(scope boyan.Scope, recordType string, id string) ([]byte, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return boyan.TupleKey(badgerSessionMemoryNamespace, badgerSessionMemoryVersion, recordType, scope.Key, string(scope.Kind), id)
}

func badgerProjectionManifestKey(scope boyan.Scope, projectionID, generationID string) ([]byte, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return boyan.TupleKey(badgerSessionMemoryNamespace, badgerSessionMemoryVersion, badgerRecordProjectionManifest, scope.Key, string(scope.Kind), projectionID, generationID)
}

func badgerProjectionRetentionKey(scope boyan.Scope, projectionID string) ([]byte, error) {
	return badgerSessionMemoryKey(scope, badgerRecordProjectionRetention, projectionID)
}

func badgerProjectionCheckpointKey(scope boyan.Scope, kind boyan.ScopeCheckpointKind) ([]byte, error) {
	return badgerSessionMemoryKey(scope, badgerRecordProjectionCheckpoint, string(kind))
}

func badgerSessionMemoryPrefix(scope boyan.Scope, recordType string) ([]byte, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return boyan.TupleKey(badgerSessionMemoryNamespace, badgerSessionMemoryVersion, recordType, scope.Key, string(scope.Kind))
}

func badgerScopeChangePrefix(scope boyan.Scope) ([]byte, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return boyan.TupleKey(badgerSessionMemoryNamespace, badgerSessionMemoryVersion, badgerRecordChange, scope.Key, string(scope.Kind))
}

func badgerScopeChangeKey(scope boyan.Scope, sequence uint64) ([]byte, error) {
	prefix, err := badgerScopeChangePrefix(scope)
	if err != nil {
		return nil, err
	}
	key := make([]byte, len(prefix)+8)
	copy(key, prefix)
	binary.BigEndian.PutUint64(key[len(prefix):], sequence)
	return key, nil
}
