package boyan_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/internal/embedding"
)

func TestNormalizedEmbeddingConfigConstructsProviderWithCaseInsensitiveScheme(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"model":"intfloat/multilingual-e5-base",` +
			`"data":[{"index":0,"embedding":[3,4]}]}`))
	}))
	t.Cleanup(server.Close)

	config := validEmbeddingsConfig()
	config.Endpoint = "HTTP" + strings.TrimPrefix(server.URL, "http")
	config.Dimensions = 2

	normalized, err := config.Normalize()
	requireNoError(t, err, "Normalize() error = %v")

	provider, err := embedding.NewOpenAIProvider(embedding.OpenAIOptions{
		Endpoint: normalized.Endpoint, Model: normalized.Model, Dimensions: normalized.Dimensions,
	})
	requireNoError(t, err, "NewOpenAIProvider() error = %v")

	if _, err := provider.Embed(context.Background(), []string{"text"}); err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
}
