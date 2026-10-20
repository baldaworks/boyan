package badger

import (
	"context"
	"errors"

	"github.com/dgraph-io/badger/v4"

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

	source, err := r.traceSourceIdentity(scope, ref, sourceID)
	if err != nil {
		return memory.SourceRecord{}, err
	}

	payload, err := r.loadTracePayload(ctx, source.Payload, maxCanonicalValueBytes)
	if err != nil {
		return memory.SourceRecord{}, err
	}

	if record, ok, err := migratedTraceSourceRecord(payload, scope); ok || err != nil {
		return record, err
	}

	return r.nativeTraceSourceRecord(ctx, scope, ref, source, payload)
}

func (r *CanonicalReader) traceSourceIdentity(scope memory.Scope,
	ref memory.SourceRef,
	sourceID string) (memory.CanonicalSourceRecord,
	error) {
	source, err := r.loadCanonicalTraceSource(scope, sourceID)
	if errors.Is(err, badger.ErrKeyNotFound) && sourceID != ref.ExportID {
		source, err = r.loadCanonicalTraceSource(scope, ref.ExportID)
	}

	if err != nil {
		return memory.CanonicalSourceRecord{}, err
	}

	if source.Scope != scope || source.SourceID != sourceID && source.SourceID != ref.ExportID {
		return memory.CanonicalSourceRecord{},
			memory.PermanentError(memory.CodeScopeViolation,
				"trace source identity does not match",
				nil)
	}

	return source, nil
}
