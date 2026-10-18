package canonicalsql

import (
	"context"
	"slices"
	"sort"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func hydrateTraceGraph(ctx context.Context, tx *Tx, request memory.TraceRequest, graph *traceGraph) error {
	if err := recheckTraceGraph(ctx, tx, *graph); err != nil {
		return err
	}

	for id, source := range graph.captured.sources {
		record, err := loadTraceSourceRecord(ctx, tx, source)
		if err != nil {
			return err
		}

		graph.rawByID[id] = record
		graph.sourcesByRef[record.Ref] = record
	}

	for i := range graph.nodes {
		node := &graph.nodes[i]

		payload, err := readerPayload(ctx, tx, node.view.revision.Payload)
		if err != nil {
			return err
		}

		node.text = payload
		if compat, ok := canonicalCompatibilityPayload(payload, request.Scope); ok {
			node.view.compat = &compat
		}

		seen := make(map[string]bool)
		for _, ref := range node.view.revision.Evidence {
			if seen[ref.SourceID] {
				continue
			}

			seen[ref.SourceID] = true
			node.view.sourceIDs = append(node.view.sourceIDs, ref.SourceID)
			node.view.sourceRefs = append(node.view.sourceRefs, graph.rawByID[ref.SourceID].Ref)
		}
	}

	return ctx.Err()
}

func loadTraceSourceRecord(ctx context.Context, tx *Tx,
	source memory.CanonicalSourceRecord) (memory.SourceRecord, error) {
	payload, err := readerPayload(ctx, tx, source.Payload)
	if err != nil {
		return memory.SourceRecord{}, err
	}

	if record, recognized, err := migratedTraceSourceRecord(payload, source.Scope); recognized || err != nil {
		if err != nil {
			return memory.SourceRecord{}, err
		}

		ids := make([]string, 0, len(record.Turn.Messages))
		for _, message := range record.Turn.Messages {
			ids = append(ids, message.MessageID)
		}

		if err := validateTraceSourceMembers(ctx, tx, source, ids, true); err != nil {
			return memory.SourceRecord{}, err
		}

		return record, nil
	}

	return nativeTraceSourceRecord(ctx, tx, source, payload)
}

func nativeTraceSourceRecord(ctx context.Context, tx *Tx, source memory.CanonicalSourceRecord,
	payload []byte) (memory.SourceRecord, error) {
	header, err := decodeCanonicalTraceTurnPayload(payload)
	if err != nil {
		return memory.SourceRecord{}, err
	}

	ids, err := traceMessageIDs(ctx, tx, source)
	if err != nil {
		return memory.SourceRecord{}, err
	}

	if err := validateTraceSourceMembers(ctx, tx, source, ids, false); err != nil {
		return memory.SourceRecord{}, err
	}

	messages, err := loadTraceMessages(ctx, tx, source, ids)
	if err != nil {
		return memory.SourceRecord{}, err
	}

	turn, err := nativeTraceTurn(source.Scope, source.SourceID, header, messages)
	if err != nil {
		return memory.SourceRecord{}, err
	}

	ref := memory.SourceRef{Scope: source.Scope, ExportID: header.ExportID,
		ConversationID: header.ConversationID, SourceTurnID: header.SourceTurnID}
	record := memory.SourceRecord{SchemaVersion: memory.DerivedSchemaVersionV1, Ref: ref,
		State: memory.SourceStateActive, Turn: &turn}

	return record, record.Validate()
}

func traceMessageIDs(ctx context.Context, tx *Tx, source memory.CanonicalSourceRecord) ([]string, error) {
	certificate, found, err := loadTurnCertificate(ctx, tx, source.Scope, source.SourceID)
	if err != nil {
		return nil, err
	}

	if found {
		return certificate.MessageIDs, nil
	}

	record, found, err := loadSourcePrivacy(ctx, tx, source)
	if err != nil {
		return nil, err
	}

	if !found || record.Preparing || !record.MessageMembershipComplete || len(record.MessageIDs) == 0 {
		return nil, readerDenied()
	}

	return record.MessageIDs, nil
}

// Bound the actual source partition as well as the captured manifest. An extra
// member cannot borrow a previous source-wide standard classification.
func validateTraceSourceMembers(ctx context.Context, tx *Tx, source memory.CanonicalSourceRecord,
	ids []string, embedded bool) error {
	if len(ids) == 0 || len(ids) > maxTurnProofMessages {
		return readerDenied()
	}

	rows, err := tx.MessagesBySource(ctx, source.Scope, []byte(source.SourceID), nil, maxTurnProofMessages+1)
	if err != nil {
		return err
	}

	if len(rows) > len(ids) || (!embedded && len(rows) != len(ids)) {
		return readerDenied()
	}

	if err := validateTraceMemberRows(rows, source, ids); err != nil {
		return err
	}

	for _, id := range ids {
		if err := validatePrivacyMemberBinding(ctx, tx, source, id); err != nil {
			return err
		}
	}

	return nil
}

func loadTraceMessages(ctx context.Context, tx *Tx, source memory.CanonicalSourceRecord,
	ids []string) ([]memory.Message, error) {
	messages := make([]memory.Message, 0, len(ids))
	remaining := uint32(memory.MaxDerivedTurnTextBytes)

	for _, id := range ids {
		record, err := loadTraceMessage(ctx, tx, source, id)
		if err != nil {
			return nil, err
		}

		if record.Payload.ByteSize > remaining {
			return nil, limitError()
		}

		data, err := readerPayload(ctx, tx, record.Payload)
		if err != nil {
			return nil, err
		}

		remaining -= record.Payload.ByteSize
		messages = append(messages, memory.Message{MessageID: id, Role: record.Role, Text: string(data)})
	}

	sort.Slice(messages, func(i, j int) bool {
		left, right := traceRoleOrder(messages[i].Role), traceRoleOrder(messages[j].Role)

		return left < right || left == right && messages[i].MessageID < messages[j].MessageID
	})

	return messages, nil
}

const traceOtherRoleOrder = 2

func traceRoleOrder(role memory.MessageRole) int {
	switch role {
	case memory.MessageRoleUser:
		return 0
	case memory.MessageRoleAssistant:
		return 1
	default:
		return traceOtherRoleOrder
	}
}

func validateTraceMemberRows(rows []Row, source memory.CanonicalSourceRecord, ids []string) error {
	for _, row := range rows {
		var message memory.MessageRecord
		if err := Decode(Message, row.Envelope, &message); err != nil {
			return err
		}

		if message.Validate() != nil || message.SourceID != source.SourceID ||
			message.MessageID != string(row.Key.Parts[0]) || !slices.Contains(ids, message.MessageID) {
			return invalidSourcePrivacy()
		}
	}

	return nil
}

func loadTraceMessage(ctx context.Context, tx *Tx, source memory.CanonicalSourceRecord,
	id string) (memory.MessageRecord, error) {
	key, err := privacyKey(source.Scope, Message, id)
	if err != nil {
		return memory.MessageRecord{}, err
	}

	var record memory.MessageRecord
	if err := getPrivacyRecord(ctx, tx, key, Message, &record); err != nil {
		return memory.MessageRecord{}, err
	}

	if record.Validate() != nil || record.SourceID != source.SourceID || record.MessageID != id {
		return memory.MessageRecord{}, invalidSourcePrivacy()
	}

	return record, nil
}
