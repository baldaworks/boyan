package clienthttp

import (
	"bytes"
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/url"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (c *Client) do(
	ctx context.Context, path string, payload any, expectedExportID string,
) (memory.IngestReceipt, error) {
	if c == nil || c.base == nil || c.client == nil {
		return memory.IngestReceipt{}, memory.PermanentError(memory.CodeDisabled, "http ingest client is unavailable", nil)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return memory.IngestReceipt{}, memory.PermanentError(memory.CodeInvalidDerived, "client request body is invalid", err)
	}

	req, err := stdhttp.NewRequestWithContext(

		ctx,

		stdhttp.MethodPost,

		c.base.ResolveReference(&url.URL{Path: path}).String(),

		bytes.NewReader(body),
	)
	if err != nil {
		return memory.IngestReceipt{}, memory.RetryableError(memory.CodeUnavailable, "client request creation failed", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return memory.IngestReceipt{}, transportError("client request failed", err)
	}
	defer resp.Body.Close()

	return decodeIngestResponse(resp, expectedExportID)
}

func decodeIngestResponse(resp *stdhttp.Response, expectedExportID string) (memory.IngestReceipt, error) {
	if resp.StatusCode == stdhttp.StatusOK {
		var receipt memory.IngestReceipt
		if err := decodeResponse(resp.Body, &receipt); err != nil {
			return memory.IngestReceipt{}, memory.PermanentError(

				memory.CodeInvalidDerived,

				"client response body is invalid",

				err,
			)
		}

		if err := receipt.Validate(); err != nil {
			return memory.IngestReceipt{}, err
		}

		if receipt.ExportID != expectedExportID {
			return memory.IngestReceipt{}, memory.PermanentError(
				memory.CodeInvalidDerived,
				"client response export identity does not match request",
				nil,
			)
		}

		return receipt, nil
	}

	failure := memory.IngestErrorResponse{}
	if err := decodeResponse(resp.Body, &failure); err != nil {
		return memory.IngestReceipt{}, classifyStatus(resp.StatusCode, "", memory.ErrorClassPermanent)
	}

	if err := failure.Validate(); err != nil {
		return memory.IngestReceipt{}, classifyStatus(resp.StatusCode, "", memory.ErrorClassPermanent)
	}

	return memory.IngestReceipt{}, classifyStatus(resp.StatusCode, failure.Code, failure.Class)
}
