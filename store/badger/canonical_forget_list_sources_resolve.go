package badger

import (
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func listedCanonicalSourceRef(
	txn *badger.Txn,
	scope memory.Scope,
	source memory.CanonicalSourceRecord,
) (memory.SourceRef, bool, error) {
	denied, err := isDeniedCanonicalSource(txn, scope, source.SourceID)
	if err != nil {
		return memory.SourceRef{}, false, err
	}

	if denied {
		return memory.SourceRef{}, false, nil
	}

	payload, err := listedCanonicalSourcePayload(txn, source.Payload.ID)
	if err != nil {
		return memory.SourceRef{}, false, err
	}

	if !isBadgerPayloadValid(payload, source.Payload) {
		return memory.SourceRef{}, false, memory.PermanentError(
			memory.CodeStoreFailure,
			"canonical source payload is invalid",
			nil,
		)
	}

	ref, _, ok := canonicalSourceRef(payload, scope, source.SourceID)
	if !ok {
		return memory.SourceRef{}, false, memory.PermanentError(
			memory.CodeInvalidDerived,
			"canonical source identity is not representable",
			nil,
		)
	}

	return ref, true, nil
}

func isDeniedCanonicalSource(txn *badger.Txn, scope memory.Scope, sourceID string) (bool, error) {
	deniedKey, err := canonicalKey(scope, badgerRecordDeniedSource, sourceID)
	if err != nil {
		return false, err
	}

	var denied badgerDeniedSource

	deniedErr := getCanonicalRecord(txn, deniedKey, badgerRecordDeniedSource, &denied)
	if deniedErr == nil {
		return true, nil
	}

	if errors.Is(deniedErr, badger.ErrKeyNotFound) {
		return false, nil
	}

	return false, deniedErr
}

func listedCanonicalSourcePayload(txn *badger.Txn, payloadID string) ([]byte, error) {
	payloadKey, err := canonicalKey(payloadScope(), badgerRecordPayload, payloadID)
	if err != nil {
		return nil, err
	}

	var payload []byte
	if err := getCanonicalRecord(txn, payloadKey, badgerRecordPayload, &payload); err != nil {
		return nil, err
	}

	return payload, nil
}
