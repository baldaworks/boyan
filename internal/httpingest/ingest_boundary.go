package httpingest

import (
	"context"
	stdhttp "net/http"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func boundaryIngestHandler(cfg Config) stdhttp.HandlerFunc {
	return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		serveJSON(w, r, cfg.RequestTimeout, cfg.MaxBodyBytes, func(ctx context.Context, body []byte) (memory.IngestReceipt, error) {
			var envelope memory.IngestEnvelope[memory.Boundary]
			if err := decodeEnvelope(body, &envelope); err != nil {
				return memory.IngestReceipt{}, err
			}
			if err := envelope.Validate(func(boundary memory.Boundary) error { return boundary.Validate() }); err != nil {
				return memory.IngestReceipt{}, err
			}
			fingerprint, err := memory.BoundaryIngestFingerprint(envelope)
			if err != nil {
				return memory.IngestReceipt{}, err
			}
			outcome, err := cfg.Boundaries.ApplySequencedBoundary(ctx, memory.SequencedBoundary{
				ProducerID:    envelope.ProducerID,
				ScopeSequence: envelope.ScopeSequence,
				Fingerprint:   fingerprint,
				Boundary:      envelope.Payload,
			})
			if err != nil {
				return memory.IngestReceipt{}, err
			}
			return memory.IngestReceiptFromOutcome(outcome)
		})
	}
}
