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

	"github.com/baldaworks/boyan"
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
		return nil, boyan.PermanentError(boyan.CodeInvalidDerived, "client base URL is required", nil)
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, boyan.PermanentError(boyan.CodeInvalidDerived, "client base URL is invalid", err)
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

func (c *Client) IngestTurn(ctx context.Context, envelope boyan.IngestEnvelope[boyan.Turn]) (boyan.IngestReceipt, error) {
	if err := envelope.Validate(func(turn boyan.Turn) error { return turn.Validate() }); err != nil {
		return boyan.IngestReceipt{}, err
	}
	return c.do(ctx, "/v1/ingest/turns", envelope)
}

func (c *Client) IngestBoundary(ctx context.Context, envelope boyan.IngestEnvelope[boyan.Boundary]) (boyan.IngestReceipt, error) {
	if err := envelope.Validate(func(boundary boyan.Boundary) error { return boundary.Validate() }); err != nil {
		return boyan.IngestReceipt{}, err
	}
	return c.do(ctx, "/v1/ingest/boundaries", envelope)
}

func (c *Client) Live(ctx context.Context) error {
	return c.probe(ctx, "/livez")
}

func (c *Client) Ready(ctx context.Context) error {
	return c.probe(ctx, "/readyz")
}

func (c *Client) do(ctx context.Context, path string, payload any) (boyan.IngestReceipt, error) {
	if c == nil || c.base == nil || c.client == nil {
		return boyan.IngestReceipt{}, boyan.PermanentError(boyan.CodeDisabled, "http ingest client is unavailable", nil)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return boyan.IngestReceipt{}, boyan.PermanentError(boyan.CodeInvalidDerived, "client request body is invalid", err)
	}
	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodPost, c.base.ResolveReference(&url.URL{Path: path}).String(), bytes.NewReader(body))
	if err != nil {
		return boyan.IngestReceipt{}, boyan.RetryableError(boyan.CodeUnavailable, "client request creation failed", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return boyan.IngestReceipt{}, boyan.RetryableError(boyan.CodeUnavailable, "client request failed", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == stdhttp.StatusOK {
		var receipt boyan.IngestReceipt
		if err := decodeResponse(resp.Body, &receipt); err != nil {
			return boyan.IngestReceipt{}, boyan.PermanentError(boyan.CodeInvalidDerived, "client response body is invalid", err)
		}
		if err := receipt.Validate(); err != nil {
			return boyan.IngestReceipt{}, err
		}
		return receipt, nil
	}
	failure := boyan.IngestErrorResponse{}
	if err := decodeResponse(resp.Body, &failure); err != nil {
		return boyan.IngestReceipt{}, classifyStatus(resp.StatusCode, "", boyan.ErrorClassPermanent)
	}
	if err := failure.Validate(); err != nil {
		return boyan.IngestReceipt{}, classifyStatus(resp.StatusCode, "", boyan.ErrorClassPermanent)
	}
	return boyan.IngestReceipt{}, classifyStatus(resp.StatusCode, failure.Code, failure.Class)
}

func (c *Client) probe(ctx context.Context, path string) error {
	if c == nil || c.base == nil || c.client == nil {
		return boyan.PermanentError(boyan.CodeDisabled, "http ingest client is unavailable", nil)
	}
	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodGet, c.base.ResolveReference(&url.URL{Path: path}).String(), nil)
	if err != nil {
		return boyan.RetryableError(boyan.CodeUnavailable, "health probe request creation failed", err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return boyan.RetryableError(boyan.CodeUnavailable, "health probe failed", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == stdhttp.StatusOK {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	var failure boyan.IngestErrorResponse
	if err := json.Unmarshal(body, &failure); err == nil && failure.Code != "" && failure.Class != "" {
		return classifyStatus(resp.StatusCode, failure.Code, failure.Class)
	}
	return classifyStatus(resp.StatusCode, "", boyan.ErrorClassPermanent)
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

func classifyStatus(status int, code boyan.ErrorCode, class boyan.ErrorClass) error {
	if code != "" && class != "" {
		switch class {
		case boyan.ErrorClassRetryable:
			return boyan.RetryableError(code, "http ingest request failed", nil)
		case boyan.ErrorClassPermanent:
			return boyan.PermanentError(code, "http ingest request failed", nil)
		}
	}
	switch status {
	case stdhttp.StatusConflict:
		return boyan.RetryableError(boyan.CodeConflict, "http ingest request failed", nil)
	case stdhttp.StatusTooManyRequests, stdhttp.StatusServiceUnavailable:
		return boyan.RetryableError(boyan.CodeUnavailable, "http ingest request failed", nil)
	case stdhttp.StatusGatewayTimeout:
		return boyan.RetryableError(boyan.CodeTimeout, "http ingest request failed", nil)
	case stdhttp.StatusBadRequest:
		return boyan.PermanentError(boyan.CodeInvalidDerived, "http ingest request failed", nil)
	default:
		return boyan.PermanentError(boyan.CodePermanent, "http ingest request failed", nil)
	}
}
