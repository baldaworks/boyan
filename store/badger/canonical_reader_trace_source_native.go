package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (r *CanonicalReader) nativeTraceSourceRecord(
	ctx context.Context,
	scope memory.Scope,
	ref memory.SourceRef,
	source memory.CanonicalSourceRecord,
	payload []byte,
) (memory.SourceRecord, error) {
	turnPayload, err := decodeCanonicalTraceTurnPayload(payload)
	if err != nil {
		return memory.SourceRecord{}, err
	}
	messages, err := r.loadSourceMessages(ctx, scope, source.SourceID)
	if err != nil {
		return memory.SourceRecord{}, err
	}
	turn, err := nativeTraceTurn(scope, source.SourceID, turnPayload, messages)
	if err != nil {
		return memory.SourceRecord{}, err
	}
	record := memory.SourceRecord{
		SchemaVersion: memory.DerivedSchemaVersionV1,
		Ref:           ref,
		State:         memory.SourceStateActive,
		Turn:          &turn,
	}
	if err := record.Validate(); err != nil {
		return memory.SourceRecord{}, err
	}
	return record, nil
}
