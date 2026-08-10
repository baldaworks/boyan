package badger

import (
	"encoding/binary"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

const (
	canonicalNamespace               byte = 0x53
	canonicalVersion                 byte = 1
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

func badgerScopeKey(scope memory.Scope) ([]byte, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return memory.TupleKey(canonicalNamespace, canonicalVersion, badgerRecordScope, scope.Key, string(scope.Kind))
}

func badgerProvenanceKey(scope memory.Scope, recordType, revisionID, relatedRevisionID string) ([]byte, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return memory.TupleKey(canonicalNamespace, canonicalVersion, recordType, scope.Key, string(scope.Kind), revisionID, relatedRevisionID)
}

func badgerProvenancePrefix(scope memory.Scope, recordType, revisionID string) ([]byte, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return memory.TupleKey(canonicalNamespace, canonicalVersion, recordType, scope.Key, string(scope.Kind), revisionID)
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

func canonicalKey(scope memory.Scope, recordType string, id string) ([]byte, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return memory.TupleKey(canonicalNamespace, canonicalVersion, recordType, scope.Key, string(scope.Kind), id)
}

func badgerProjectionManifestKey(scope memory.Scope, projectionID, generationID string) ([]byte, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return memory.TupleKey(canonicalNamespace, canonicalVersion, badgerRecordProjectionManifest, scope.Key, string(scope.Kind), projectionID, generationID)
}

func badgerProjectionRetentionKey(scope memory.Scope, projectionID string) ([]byte, error) {
	return canonicalKey(scope, badgerRecordProjectionRetention, projectionID)
}

func badgerProjectionCheckpointKey(scope memory.Scope, kind memory.ScopeCheckpointKind) ([]byte, error) {
	return canonicalKey(scope, badgerRecordProjectionCheckpoint, string(kind))
}

func canonicalPrefix(scope memory.Scope, recordType string) ([]byte, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return memory.TupleKey(canonicalNamespace, canonicalVersion, recordType, scope.Key, string(scope.Kind))
}

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
