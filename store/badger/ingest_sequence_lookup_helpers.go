package badger

import (
	"context"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func isCanonicalProducerID(value string) bool {
	return value != "" && value == strings.TrimSpace(value) &&
		!strings.ContainsAny(value, "\r\n\t")
}

func openIngestSequenceStore(
	ctx context.Context,
	store *CanonicalStore,
	producerID string,
	scope memory.Scope,
) (*CanonicalStore, func(), error) {
	if err := canonicalContextError(ctx); err != nil {
		return nil, nil, err
	}

	if err := scope.Validate(); err != nil {
		return nil, nil, err
	}

	if !isCanonicalProducerID(producerID) {
		return nil, nil, memory.PermanentError(memory.CodeInvalidDerived, "ingest producer id is invalid", nil)
	}

	if store == nil {
		return nil, nil, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}

	store.maintenanceMu.RLock()

	if store.db == nil {
		store.maintenanceMu.RUnlock()

		return nil, nil, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}

	return store, store.maintenanceMu.RUnlock, nil
}
