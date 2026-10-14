package clienthttp

import (
	"context"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"net/url"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (c *Client) Live(ctx context.Context) error {
	return c.probe(ctx, "/livez")
}

func (c *Client) Ready(ctx context.Context) error {
	return c.probe(ctx, "/readyz")
}

func (c *Client) probe(ctx context.Context, path string) error {
	if c == nil || c.base == nil || c.client == nil {
		return memory.PermanentError(memory.CodeDisabled, "http ingest client is unavailable", nil)
	}

	req, err := stdhttp.NewRequestWithContext(

		ctx,

		stdhttp.MethodGet,

		c.base.ResolveReference(
			&url.URL{
				Path: path,
			},
		).String(),

		nil,
	)
	if err != nil {
		return memory.RetryableError(memory.CodeUnavailable, "health probe request creation failed", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return transportError("health probe failed", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == stdhttp.StatusOK {
		return nil
	}

	body, _ := io.ReadAll(resp.Body)

	var failure memory.IngestErrorResponse
	if err := json.Unmarshal(body, &failure); err == nil && failure.Validate() == nil {
		return classifyStatus(resp.StatusCode, failure.Code, failure.Class)
	}

	return classifyStatus(resp.StatusCode, "", memory.ErrorClassPermanent)
}
