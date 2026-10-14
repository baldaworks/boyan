package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// Check both declared content and encoded Badger value bounds before copying.
func (r *CanonicalReader) loadTracePayload(ctx context.Context, ref memory.PayloadRef, limit uint32) ([]byte, error) {
	if err := canonicalReaderContext(ctx); err != nil {
		return nil, err
	}

	if err := ref.Validate(); err != nil {
		return nil, err
	}

	if ref.ByteSize > limit {
		return nil, memory.PermanentError(memory.CodeLimitExceeded, "trace payload exceeds its bound", nil)
	}

	if r.beforeTracePayloadRead != nil {
		r.beforeTracePayloadRead()
	}

	r.store.maintenanceMu.RLock()
	defer r.store.maintenanceMu.RUnlock()

	if r.store.db == nil {
		return nil, invalidSourcePrivacy()
	}

	key, err := payloadRecordKey(ref)
	if err != nil {
		return nil, err
	}

	var data []byte

	err = r.store.db.View(func(txn *badger.Txn) error {
		return getPrivacyPreparationRecord(txn, key, badgerRecordPayload, &data)
	})
	if err != nil {
		return nil, canonicalStoreError("load trace payload", err)
	}

	if !isBadgerPayloadValid(data, ref) {
		return nil, invalidSourcePrivacy()
	}

	return data, nil
}
