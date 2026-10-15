package embedding

import (
	"context"
	"net/http"
	"testing"
)

func TestOpenAIProviderRequestCannotBeReplayedByHTTPTransport(t *testing.T) {
	provider, err := NewOpenAIProvider(OpenAIOptions{
		Endpoint: "https://provider.example/v1/embeddings", Model: "m", Dimensions: 2,
	})
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}

	request, err := provider.(*openAIProvider).request(context.Background(), []byte(`{"input":["text"]}`))
	if err != nil {
		t.Fatalf("request() error = %v", err)
	}

	// net/http may transparently retry a request on a reused connection when
	// nothing was written if GetBody can rewind it. A nil GetBody forbids that.
	if request.GetBody != nil {
		t.Fatal("request.GetBody is non-nil, allowing transparent transport replay")
	}

	transport, ok := provider.(*openAIProvider).httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("HTTP transport = %T, want owned *http.Transport", provider.(*openAIProvider).httpClient.Transport)
	}

	if !transport.DisableKeepAlives {
		t.Fatal("HTTP transport permits connection reuse, allowing reused-connection retries")
	}

	if transport.Protocols == nil || !transport.Protocols.HTTP1() ||
		transport.Protocols.HTTP2() || transport.Protocols.UnencryptedHTTP2() {
		t.Fatalf("HTTP protocols = %v, want HTTP/1 only to exclude HTTP/2 transparent retries", transport.Protocols)
	}
}

func TestOpenAIProviderTransportDoesNotUseAmbientProxy(t *testing.T) {
	provider, err := NewOpenAIProvider(OpenAIOptions{
		Endpoint: "https://provider.example/v1/embeddings", Model: "m", Dimensions: 2,
	})
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}

	transport := provider.(*openAIProvider).httpClient.Transport.(*http.Transport)
	if transport.Proxy != nil {
		t.Fatal("HTTP transport uses ambient proxy configuration for secret-bearing requests")
	}
}
