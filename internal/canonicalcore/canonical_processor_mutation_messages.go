package canonicalcore

import . "github.com/baldaworks/boyan/pkg/memory"

func (p *CanonicalTurnProcessor) appendCanonicalTurnMessages(
	mutation *CanonicalMutation,
	sourceID string,
	messages []Message,
) (
	map[string]Message,
	error,
) {
	messageByID := make(map[string]Message, len(messages))
	for _, message := range messages {
		if message.MessageID == "" {
			return nil, invalidDerived("canonical turn message identity is required")
		}

		if _, exists := messageByID[message.MessageID]; exists {
			return nil, invalidDerived("canonical turn message identity is duplicated")
		}

		messageByID[message.MessageID] = message

		payload, payloadRef, err := p.payload(
			canonicalScopedPayloadID(mutation.Scope, "message", message.MessageID), []byte(message.Text),
		)
		if err != nil {
			return nil, err
		}

		mutation.Messages = append(mutation.Messages, MessageRecord{
			MessageID: message.MessageID,
			SourceID:  sourceID,
			Role:      message.Role,
			Payload:   payloadRef,
		})
		mutation.Payloads = append(mutation.Payloads, payload)
	}

	return messageByID, nil
}
