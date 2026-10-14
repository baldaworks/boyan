package clienthttp

import (
	stdhttp "net/http"
	"net/url"
	"strings"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

const defaultClientTimeout = 15 * time.Second

// Config wires the typed Boyan standalone HTTP client.
type Config struct {
	BaseURL string
	Client  *stdhttp.Client
	Timeout time.Duration
}

// Client is the typed HTTP client for the Boyan standalone HTTP surface.
type Client struct {
	base   *url.URL
	client *stdhttp.Client
}

const maxRedirects = 10

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

	clientCopy := *httpClient
	configuredRedirect := clientCopy.CheckRedirect
	clientCopy.CheckRedirect = func(req *stdhttp.Request, via []*stdhttp.Request) error {
		if !sameOrigin(parsed, req.URL) {
			return stdhttp.ErrUseLastResponse
		}

		if configuredRedirect != nil {
			return configuredRedirect(req, via)
		}

		if len(via) >= maxRedirects {
			return stdhttp.ErrUseLastResponse
		}

		return nil
	}

	return &Client{base: parsed, client: &clientCopy}, nil
}

func sameOrigin(left, right *url.URL) bool {
	return left != nil && right != nil &&
		strings.EqualFold(left.Scheme, right.Scheme) &&
		strings.EqualFold(left.Host, right.Host)
}
