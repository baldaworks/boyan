package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Inspect only bounded structural metadata before any raw payload copy.
func tracePayloadBounds(ctx context.Context, tx *Tx, captured readerSnapshot) error {
	for _, source := range captured.sources {
		if source.Payload.ByteSize > MaxEnvelopeBytes {
			return limitError()
		}

		rows, err := tx.MessagesBySource(ctx, source.Scope, []byte(source.SourceID), nil, maxTurnProofMessages+1)
		if err != nil {
			return err
		}

		if len(rows) > maxTurnProofMessages {
			return readerDenied()
		}

		remaining := uint32(memory.MaxDerivedTurnTextBytes)

		for _, row := range rows {
			var message memory.MessageRecord
			if err := Decode(Message, row.Envelope, &message); err != nil {
				return err
			}

			if message.Payload.ByteSize > remaining {
				return limitError()
			}

			remaining -= message.Payload.ByteSize
		}
	}

	return ctx.Err()
}
