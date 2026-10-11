package httpingest

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
	stdhttp "net/http"
)

func turnIngestHandler(cfg Config) stdhttp.HandlerFunc {
	return ingestHandler(cfg, memory.TurnIngestFingerprint, func(
		ctx context.Context,
		envelope memory.IngestEnvelope[memory.Turn],
		fingerprint string,
	) (
		memory.IngestOutcome,
		error,
	) {
		return cfg.Turns.IngestSequencedTurn(ctx, memory.SequencedTurn{
			ProducerID: envelope.ProducerID, ScopeSequence: envelope.ScopeSequence,
			Fingerprint: fingerprint, Turn: envelope.Payload,
		})
	})
}
