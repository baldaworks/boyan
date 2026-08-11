package clienthttp

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (c *Client) IngestTurn(ctx context.Context, envelope memory.IngestEnvelope[memory.Turn]) (memory.IngestReceipt, error) {
	if err := envelope.Validate(func(turn memory.Turn) error { return turn.Validate() }); err != nil {
		return memory.IngestReceipt{}, err
	}
	return c.do(ctx, "/v1/ingest/turns", envelope)
}

func (c *Client) IngestBoundary(ctx context.Context, envelope memory.IngestEnvelope[memory.Boundary]) (memory.IngestReceipt, error) {
	if err := envelope.Validate(func(boundary memory.Boundary) error { return boundary.Validate() }); err != nil {
		return memory.IngestReceipt{}, err
	}
	return c.do(ctx, "/v1/ingest/boundaries", envelope)
}
