package clienthttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	stdhttp "net/http"
	"net/url"
	"strings"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

const defaultClientTimeout = 15 * time.Second

// Config wires the typed Boyan HTTP ingest client.
type Config struct {
	BaseURL string
	Client  *stdhttp.Client
	Timeout time.Duration
}

// Client is the typed HTTP client for the Boyan ingest API.
type Client struct {
	base   *url.URL
	client *stdhttp.Client
}

func New(cfg Config) (*Client, error) {
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		return nil, memory.PermanentError(memory.CodeInvalidDerived, "client base URL is required", nil)
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, memory.PermanentError(memory.CodeInvalidDerived, "client base URL is invalid", err)
	}
	httpClient := cfg.Client
	if httpClient == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = defaultClientTimeout
		}
		httpClient = &stdhttp.Client{Timeout: timeout}
	}
	return &Client{base: parsed, client: httpClient}, nil
}

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

func (c *Client) Live(ctx context.Context) error {
	return c.probe(ctx, "/livez")
}

func (c *Client) Ready(ctx context.Context) error {
	return c.probe(ctx, "/readyz")
}

func (c *Client) do(ctx context.Context, path string, payload any) (memory.IngestReceipt, error) {
	if c == nil || c.base == nil || c.client == nil {
		return memory.IngestReceipt{}, memory.PermanentError(memory.CodeDisabled, "http ingest client is unavailable", nil)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return memory.IngestReceipt{}, memory.PermanentError(memory.CodeInvalidDerived, "client request body is invalid", err)
	}
	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodPost, c.base.ResolveReference(&url.URL{Path: path}).String(), bytes.NewReader(body))
	if err != nil {
		return memory.IngestReceipt{}, memory.RetryableError(memory.CodeUnavailable, "client request creation failed", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return memory.IngestReceipt{}, memory.RetryableError(memory.CodeUnavailable, "client request failed", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == stdhttp.StatusOK {
		var receipt memory.IngestReceipt
		if err := decodeResponse(resp.Body, &receipt); err != nil {
			return memory.IngestReceipt{}, memory.PermanentError(memory.CodeInvalidDerived, "client response body is invalid", err)
		}
		if err := receipt.Validate(); err != nil {
			return memory.IngestReceipt{}, err
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

func (c *Client) probe(ctx context.Context, path string) error {
	if c == nil || c.base == nil || c.client == nil {
		return memory.PermanentError(memory.CodeDisabled, "http ingest client is unavailable", nil)
	}
	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodGet, c.base.ResolveReference(&url.URL{Path: path}).String(), nil)
	if err != nil {
		return memory.RetryableError(memory.CodeUnavailable, "health probe request creation failed", err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return memory.RetryableError(memory.CodeUnavailable, "health probe failed", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == stdhttp.StatusOK {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	var failure memory.IngestErrorResponse
	if err := json.Unmarshal(body, &failure); err == nil && failure.Code != "" && failure.Class != "" {
		return classifyStatus(resp.StatusCode, failure.Code, failure.Class)
	}
	return classifyStatus(resp.StatusCode, "", memory.ErrorClassPermanent)
}

func decodeResponse[T any](body io.Reader, dst *T) error {
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(new(struct{})); err != io.EOF {
		return fmt.Errorf("response contained trailing data")
	}
	return nil
}

func classifyStatus(status int, code memory.ErrorCode, class memory.ErrorClass) error {
	if code != "" && class != "" {
		switch class {
		case memory.ErrorClassRetryable:
			return memory.RetryableError(code, "http ingest request failed", nil)
		case memory.ErrorClassPermanent:
			return memory.PermanentError(code, "http ingest request failed", nil)
		}
	}
	switch status {
	case stdhttp.StatusConflict:
		return memory.RetryableError(memory.CodeConflict, "http ingest request failed", nil)
	case stdhttp.StatusTooManyRequests, stdhttp.StatusServiceUnavailable:
		return memory.RetryableError(memory.CodeUnavailable, "http ingest request failed", nil)
	case stdhttp.StatusGatewayTimeout:
		return memory.RetryableError(memory.CodeTimeout, "http ingest request failed", nil)
	case stdhttp.StatusBadRequest:
		return memory.PermanentError(memory.CodeInvalidDerived, "http ingest request failed", nil)
	default:
		return memory.PermanentError(memory.CodePermanent, "http ingest request failed", nil)
	}
}
