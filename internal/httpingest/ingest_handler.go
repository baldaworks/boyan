package httpingest

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
	stdhttp "net/http"
)

func ingestHandler[P interface{ Validate() error }](
	cfg Config,
	fingerprint func(memory.IngestEnvelope[P]) (string, error),
	apply func(context.Context, memory.IngestEnvelope[P], string) (memory.IngestOutcome, error),
) stdhttp.HandlerFunc {
	return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		serveJSON(w, r, cfg.RequestTimeout, cfg.MaxBodyBytes, func(
			ctx context.Context,
			body []byte,
		) (memory.IngestReceipt, error) {
			var envelope memory.IngestEnvelope[P]
			if err := decodeEnvelope(body, &envelope); err != nil {
				return memory.IngestReceipt{}, &requestValidationError{err: err}
			}

			if err := envelope.Validate(
				func(
					payload P,
				) error {
					return payload.Validate()
				},
			); err != nil {
				return memory.IngestReceipt{}, &requestValidationError{err: err}
			}

			digest, err := fingerprint(envelope)
			if err != nil {
				return memory.IngestReceipt{}, err
			}

			outcome, err := apply(ctx, envelope, digest)
			if err != nil {
				return memory.IngestReceipt{}, err
			}

			return memory.IngestReceiptFromOutcome(outcome)
		})
	}
}
