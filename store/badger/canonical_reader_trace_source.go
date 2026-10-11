package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (r *CanonicalReader) loadSourceRecord(
	ctx context.Context,
	scope memory.Scope,
	ref memory.SourceRef,
	sourceID string,
) (memory.SourceRecord, error) {
	if !r.hasOpenStore() {
		return memory.SourceRecord{}, memory.PermanentError(memory.CodeStoreFailure, "canonical reader store is closed", nil)
	}

	if err := ref.Validate(); err != nil {
		return memory.SourceRecord{}, err
	}

	source, err := r.loadCanonicalTraceSource(scope, sourceID)
	if err != nil {
		return memory.SourceRecord{}, err
	}
	// Evidence keys normally use the canonical SourceID, whereas migrated
	// payloads retain the original export ID. Try both identities so the
	// adapter remains compatible with either representation.
	if source.SourceID != ref.ExportID {
		source, err = r.loadCanonicalTraceSource(scope, ref.ExportID)
		if err != nil {
			return memory.SourceRecord{}, err
		}
	}

	payload, err := r.store.LoadPayload(ctx, source.Payload)
	if err != nil {
		return memory.SourceRecord{}, err
	}

	if record, ok, err := migratedTraceSourceRecord(payload, scope); ok || err != nil {
		return record, err
	}

	return r.nativeTraceSourceRecord(ctx, scope, ref, source, payload)
}
