package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (r *CanonicalReader) loadSourceMessages(ctx context.Context, scope memory.Scope, sourceID string) ([]memory.Message, error) {
	if err := canonicalReaderContext(ctx); err != nil {
		return nil, err
	}
	records, err := r.loadSourceMessageRecords(scope, sourceID)
	if err != nil {
		return nil, err
	}
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
