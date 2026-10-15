package embedding

import (
	"net"
	"net/http"
	"time"
)

const (
	maxBatchSize     = 16
	maxInputBytes    = 2048
	maxRequestBytes  = 64 << 10
	maxResponseBytes = 1 << 20
	maxEndpointBytes = 2048
	maxModelBytes    = 256
	maxDimensions    = 4096
	maxAPIKeyBytes   = 4096
	requestTimeout   = 10 * time.Second
	maxConcurrent    = 4

	transportDialTimeout       = 30 * time.Second
	transportKeepAlive         = 30 * time.Second
	transportMaxIdleConns      = 100
	transportIdleConnTimeout   = 90 * time.Second
	transportTLSHandshakeLimit = 10 * time.Second
	transportExpectContinue    = time.Second
)

type openAIProvider struct {
	endpoint   string
	model      string
	dimensions int
	apiKey     string
	httpClient *http.Client
	slots      chan struct{}
}

// NewOpenAIProvider constructs an immutable OpenAI-compatible provider.
func NewOpenAIProvider(options OpenAIOptions) (Provider, error) {
	if !validOptions(options) {
		return nil, newError(CodeInvalidInput)
	}

	return &openAIProvider{
		endpoint:   options.Endpoint,
		model:      options.Model,
		dimensions: options.Dimensions,
		apiKey:     options.APIKey,
		httpClient: &http.Client{
			Transport: newHTTPTransport(),
			Timeout:   requestTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		slots: make(chan struct{}, maxConcurrent),
	}, nil
}

func newHTTPTransport() *http.Transport {
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)

	dialer := &net.Dialer{
		Timeout:   transportDialTimeout,
		KeepAlive: transportKeepAlive,
	}

	return &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          transportMaxIdleConns,
		IdleConnTimeout:       transportIdleConnTimeout,
		TLSHandshakeTimeout:   transportTLSHandshakeLimit,
		ExpectContinueTimeout: transportExpectContinue,
		DisableKeepAlives:     true,
		Protocols:             protocols,
	}
}
