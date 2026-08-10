package badger

import (
	"context"
	"encoding/json"
	"sort"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func (r *CanonicalReader) loadSourceRecord(ctx context.Context, scope memory.Scope, ref memory.SourceRef, sourceID string) (memory.SourceRecord, error) {
	if r == nil || r.store == nil || r.store.db == nil {
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

func (r *CanonicalReader) loadCanonicalTraceSource(scope memory.Scope, sourceID string) (memory.CanonicalSourceRecord, error) {
	var source memory.CanonicalSourceRecord
	key, err := canonicalKey(scope, badgerRecordSource, sourceID)
	if err != nil {
		return memory.CanonicalSourceRecord{}, err
	}
	if err := r.store.db.View(func(txn *badger.Txn) error {
		return getCanonicalRecord(txn, key, badgerRecordSource, &source)
	}); err != nil {
		return memory.CanonicalSourceRecord{}, canonicalStoreError("load canonical trace source", err)
	}
	return source, nil
}

func migratedTraceSourceRecord(payload []byte, scope memory.Scope) (memory.SourceRecord, bool, error) {
	var migrated memory.SourceRecord
	if err := json.Unmarshal(payload, &migrated); err == nil && migrated.Ref.Scope == scope && migrated.Ref.SessionID != "" {
		if migrated.SchemaVersion == memory.DerivedSchemaVersionV1 && migrated.State == memory.SourceStateActive && migrated.Turn != nil {
			if err := migrated.Validate(); err != nil {
				return memory.SourceRecord{}, true, memory.PermanentError(memory.CodeStoreFailure, "migrated canonical source is invalid", err)
			}
			return migrated, true, nil
		}
	}
	return memory.SourceRecord{}, false, nil
}

func (r *CanonicalReader) nativeTraceSourceRecord(ctx context.Context, scope memory.Scope, ref memory.SourceRef, source memory.CanonicalSourceRecord, payload []byte) (memory.SourceRecord, error) {
	var turnPayload canonicalTurnSourcePayload
	if err := json.Unmarshal(payload, &turnPayload); err != nil || turnPayload.ExportID == "" || turnPayload.SessionID == "" || turnPayload.SourceTurnID == "" {
		return memory.SourceRecord{}, memory.PermanentError(memory.CodeInvalidDerived, "canonical trace source payload is not representable", nil)
	}
	messages, err := r.loadSourceMessages(ctx, scope, source.SourceID)
	if err != nil {
		return memory.SourceRecord{}, err
	}
	if len(messages) == 0 {
		return memory.SourceRecord{}, memory.PermanentError(memory.CodeInvalidDerived, "canonical trace source has no messages", nil)
	}
	turn := memory.Turn{
		SchemaVersion:  memory.SchemaVersionV1,
		ExportID:       turnPayload.ExportID,
		Scope:          scope,
		Session:        memory.SessionRef{SessionID: turnPayload.SessionID, AgentSessionID: turnPayload.AgentSessionID},
		SourceTurnID:   turnPayload.SourceTurnID,
		SourceID:       source.SourceID,
		CompletedAt:    turnPayload.CompletedAt,
		TerminalStatus: turnPayload.TerminalStatus,
		Messages:       messages,
	}
	if err := turn.Validate(); err != nil {
		return memory.SourceRecord{}, memory.PermanentError(memory.CodeStoreFailure, "canonical trace source turn is invalid", err)
	}
	record := memory.SourceRecord{SchemaVersion: memory.DerivedSchemaVersionV1, Ref: ref, State: memory.SourceStateActive, Turn: &turn}
	if err := record.Validate(); err != nil {
		return memory.SourceRecord{}, err
	}
	return record, nil
}

func (r *CanonicalReader) loadSourceMessages(ctx context.Context, scope memory.Scope, sourceID string) ([]memory.Message, error) {
	if err := canonicalReaderContext(ctx); err != nil {
		return nil, err
	}
	prefix, err := canonicalPrefix(scope, badgerRecordMessage)
	if err != nil {
		return nil, err
	}
	var records []memory.MessageRecord
	if err := r.store.db.View(func(txn *badger.Txn) error {
		options := badger.DefaultIteratorOptions
		options.Prefix = prefix
		iterator := txn.NewIterator(options)
		defer iterator.Close()
		for iterator.Seek(prefix); iterator.ValidForPrefix(prefix); iterator.Next() {
			var record memory.MessageRecord
			if err := getCanonicalRecord(txn, iterator.Item().Key(), badgerRecordMessage, &record); err != nil {
				return err
			}
			if record.SourceID == sourceID {
				if len(records) >= 32 {
					return memory.PermanentError(memory.CodeLimitExceeded, "canonical trace source message bound exceeded", nil)
				}
				if err := record.Validate(); err != nil {
					return err
				}
				records = append(records, record)
			}
		}
		return nil
	}); err != nil {
		return nil, canonicalStoreError("scan canonical trace messages", err)
	}
	sort.Slice(records, func(i, j int) bool {
		return messageRoleOrder(records[i].Role) < messageRoleOrder(records[j].Role) ||
			(messageRoleOrder(records[i].Role) == messageRoleOrder(records[j].Role) && records[i].MessageID < records[j].MessageID)
	})
	messages := make([]memory.Message, 0, len(records))
	for _, record := range records {
		payload, err := r.store.LoadPayload(ctx, record.Payload)
		if err != nil {
			return nil, err
		}
		messages = append(messages, memory.Message{MessageID: record.MessageID, Role: record.Role, Text: string(payload)})
	}
	return messages, nil
}

func messageRoleOrder(role memory.MessageRole) int {
	switch role {
	case memory.MessageRoleUser:
		return 0
	case memory.MessageRoleAssistant:
		return 1
	default:
		return 2
	}
}
