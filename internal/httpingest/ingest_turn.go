package httpingest

import (
	"context"
	stdhttp "net/http"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func turnIngestHandler(cfg Config) stdhttp.HandlerFunc {
	return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		serveJSON(w, r, cfg.RequestTimeout, cfg.MaxBodyBytes, func(ctx context.Context, body []byte) (memory.IngestReceipt, error) {
			var envelope memory.IngestEnvelope[memory.Turn]
			if err := decodeEnvelope(body, &envelope); err != nil {
				return memory.IngestReceipt{}, err
			}
			if err := envelope.Validate(func(turn memory.Turn) error { return turn.Validate() }); err != nil {
				return memory.IngestReceipt{}, err
			}
			fingerprint, err := memory.TurnIngestFingerprint(envelope)
			if err != nil {
				return memory.IngestReceipt{}, err
			}
			outcome, err := cfg.Turns.IngestSequencedTurn(ctx, memory.SequencedTurn{
				ProducerID:    envelope.ProducerID,
				ScopeSequence: envelope.ScopeSequence,
				Fingerprint:   fingerprint,
				Turn:          envelope.Payload,
			})
			if err != nil {
				return memory.IngestReceipt{}, err
			}
			return memory.IngestReceiptFromOutcome(outcome)
		})
	}
}
