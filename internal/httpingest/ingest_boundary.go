package httpingest

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
	stdhttp "net/http"
)

func boundaryIngestHandler(cfg Config) stdhttp.HandlerFunc {
	return ingestHandler(cfg, memory.BoundaryIngestFingerprint, func(
		ctx context.Context,
		envelope memory.IngestEnvelope[memory.Boundary],
		fingerprint string,
	) (
		memory.IngestOutcome,
		error,
	) {
		return cfg.Boundaries.ApplySequencedBoundary(ctx, memory.SequencedBoundary{
			ProducerID: envelope.ProducerID, ScopeSequence: envelope.ScopeSequence,
			Fingerprint: fingerprint, Boundary: envelope.Payload,
		})
	})
}
