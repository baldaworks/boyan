//go:build integration

package embedding_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/baldaworks/boyan/internal/embedding"
)

func TestOpenAIProviderEmbedsFloatVectorsInInputOrder(t *testing.T) {
	t.Parallel()

	requestErr := make(chan string, 1)
	server := httptest.NewServer(validEmbeddingHandler(requestErr))
	t.Cleanup(server.Close)

	provider, err := embedding.NewOpenAIProvider(embedding.OpenAIOptions{
		Endpoint:   server.URL,
		Model:      "test-model",
		Dimensions: 2,
		APIKey:     "ephemeral-secret",
	})
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}

	vectors, err := provider.Embed(context.Background(), []string{"first", "second"})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}

	select {
	case message := <-requestErr:
		t.Fatal(message)
	default:
	}

	want := [][]float32{{0.6, 0.8}, {0, 1}}
	if !reflect.DeepEqual(vectors, want) {
		t.Fatalf("Embed() = %#v, want %#v", vectors, want)
	}
}

func TestOpenAIProviderOmitsAuthorizationWithoutAPIKey(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if _, exists := request.Header["Authorization"]; exists {
			t.Errorf("Authorization header = %q, want absent", request.Header.Get("Authorization"))
		}

		_, _ = writer.Write([]byte(validResponse()))
	}))
	t.Cleanup(server.Close)

	provider, err := embedding.NewOpenAIProvider(embedding.OpenAIOptions{
		Endpoint: server.URL, Model: "m", Dimensions: 2,
	})
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}

	if _, err := provider.Embed(context.Background(), []string{"text"}); err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
}

func validEmbeddingHandler(requestErr chan<- string) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if message := embeddingRequestError(request); message != "" {
			requestErr <- message

			return
		}

		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(
			`{"model":"test-model","data":[` +
				`{"index":1,"embedding":[0,2]},` +
				`{"index":0,"embedding":[3,4]}]}`,
		))
	})
}

func embeddingRequestError(request *http.Request) string {
	var body struct {
		Model          string   `json:"model"`
		Input          []string `json:"input"`
		EncodingFormat string   `json:"encoding_format"`
	}
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		return "request body is not JSON"
	}

	if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/json" {
		return "request method or content type does not match the OpenAI contract"
	}

	if request.Header.Get("Authorization") != "Bearer ephemeral-secret" || body.Model != "test-model" {
		return "request authorization or model does not match the OpenAI contract"
	}

	if !reflect.DeepEqual(body.Input, []string{"first", "second"}) || body.EncodingFormat != "float" {
		return "request input or encoding does not match the OpenAI contract"
	}

	return ""
}
