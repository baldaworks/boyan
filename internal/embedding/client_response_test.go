//go:build integration

package embedding_test

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/internal/embedding"
)

func TestOpenAIProviderClassifiesHTTPStatusesWithoutDisclosure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		body   string
		code   embedding.ErrorCode
	}{
		{name: "bad request", status: http.StatusBadRequest, body: "upstream-secret", code: embedding.CodeInputLimit},
		{
			name: "payload too large", status: http.StatusRequestEntityTooLarge,
			body: "upstream-secret", code: embedding.CodeInputLimit,
		},
		{
			name: "TEI validation", status: http.StatusUnprocessableEntity,
			body: `{"code":422,"type":"Validation","detail":"upstream-secret"}`,
			code: embedding.CodeInputLimit,
		},
		{
			name: "other unprocessable", status: http.StatusUnprocessableEntity,
			body: `{"code":422,"type":"Other","detail":"upstream-secret"}`,
			code: embedding.CodeUnavailable,
		},
		{name: "rate limited", status: http.StatusTooManyRequests, body: "upstream-secret", code: embedding.CodeUnavailable},
		{
			name: "server failure", status: http.StatusInternalServerError,
			body: "upstream-secret", code: embedding.CodeUnavailable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(test.status)
				_, _ = writer.Write([]byte(test.body))
			}))
			t.Cleanup(server.Close)

			options := embedding.OpenAIOptions{Endpoint: server.URL, Model: "m", Dimensions: 2, APIKey: "bearer-secret"}

			provider, err := embedding.NewOpenAIProvider(options)
			if err != nil {
				t.Fatalf("NewOpenAIProvider() error = %v", err)
			}

			_, err = provider.Embed(context.Background(), []string{"input-secret"})
			requireFailureCode(t, err, test.code)
			requireRedacted(t, err, "upstream-secret", "input-secret", "bearer-secret", server.URL)
		})
	}
}

func TestOpenAIProviderRejectsInvalidResponseShapes(t *testing.T) {
	t.Parallel()

	for _, test := range append(invalidResponseShapeCases(), invalidVectorCases()...) {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			server := responseServer(t, http.StatusOK, test.body)
			provider := newTestProvider(t, server.URL, 2)

			_, err := provider.Embed(context.Background(), []string{"input-secret"})
			requireFailureCode(t, err, test.code)
			requireRedacted(t, err, "input-secret", server.URL, test.body)
		})
	}
}

type responseFailureCase struct {
	name string
	body string
	code embedding.ErrorCode
}

func invalidResponseShapeCases() []responseFailureCase {
	return []responseFailureCase{
		{name: "malformed JSON", body: `{`, code: embedding.CodeInvalidResponse},
		{name: "trailing JSON", body: validResponse() + `{}`, code: embedding.CodeInvalidResponse},
		{name: "missing model", body: `{"data":[{"index":0,"embedding":[3,4]}]}`, code: embedding.CodeModelMismatch},
		{
			name: "wrong model", body: `{"model":"other","data":[{"index":0,"embedding":[3,4]}]}`,
			code: embedding.CodeModelMismatch,
		},
		{name: "missing data", body: `{"model":"m"}`, code: embedding.CodeInvalidResponse},
		{
			name: "too many entries",
			body: `{"model":"m","data":[{"index":0,"embedding":[3,4]},` +
				`{"index":0,"embedding":[3,4]}]}`,
			code: embedding.CodeInvalidResponse,
		},
		{name: "missing index", body: `{"model":"m","data":[{"embedding":[3,4]}]}`, code: embedding.CodeInvalidResponse},
		{
			name: "null index", body: `{"model":"m","data":[{"index":null,"embedding":[3,4]}]}`,
			code: embedding.CodeInvalidResponse,
		},
		{
			name: "negative index", body: `{"model":"m","data":[{"index":-1,"embedding":[3,4]}]}`,
			code: embedding.CodeInvalidResponse,
		},
		{
			name: "out of range index", body: `{"model":"m","data":[{"index":1,"embedding":[3,4]}]}`,
			code: embedding.CodeInvalidResponse,
		},
	}
}

func invalidVectorCases() []responseFailureCase {
	return []responseFailureCase{
		{
			name: "wrong dimension", body: `{"model":"m","data":[{"index":0,"embedding":[3]}]}`,
			code: embedding.CodeDimensionMismatch,
		},
		{
			name: "null embedding", body: `{"model":"m","data":[{"index":0,"embedding":null}]}`,
			code: embedding.CodeDimensionMismatch,
		},
		{
			name: "null component", body: `{"model":"m","data":[{"index":0,"embedding":[null,4]}]}`,
			code: embedding.CodeInvalidResponse,
		},
		{
			name: "NaN component", body: `{"model":"m","data":[{"index":0,"embedding":[NaN,4]}]}`,
			code: embedding.CodeInvalidResponse,
		},
		{
			name: "infinite component", body: `{"model":"m","data":[{"index":0,"embedding":[1e400,4]}]}`,
			code: embedding.CodeInvalidResponse,
		},
		{
			name: "zero vector", body: `{"model":"m","data":[{"index":0,"embedding":[0,0]}]}`,
			code: embedding.CodeInvalidResponse,
		},
	}
}

func TestOpenAIProviderRejectsDuplicateAndMissingIndexes(t *testing.T) {
	t.Parallel()

	body := `{"model":"m","data":[{"index":0,"embedding":[3,4]},` +
		`{"index":0,"embedding":[4,3]}]}`
	server := responseServer(t, http.StatusOK, body)
	provider := newTestProvider(t, server.URL, 2)

	_, err := provider.Embed(context.Background(), []string{"first", "second"})
	requireFailureCode(t, err, embedding.CodeInvalidResponse)
}

func TestOpenAIProviderEnforcesResponseByteLimit(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		size int
		code embedding.ErrorCode
	}{
		{name: "exact limit", size: 1 << 20},
		{name: "one over limit", size: (1 << 20) + 1, code: embedding.CodeResponseLimit},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			body := responseWithSize(t, test.size)
			server := responseServer(t, http.StatusOK, body)
			provider := newTestProvider(t, server.URL, 2)

			vectors, err := provider.Embed(context.Background(), []string{"text"})
			if test.code != "" {
				requireFailureCode(t, err, test.code)

				return
			}

			if err != nil {
				t.Fatalf("Embed() error = %v", err)
			}

			if want := [][]float32{{0.6, 0.8}}; !reflect.DeepEqual(vectors, want) {
				t.Fatalf("Embed() = %#v, want %#v", vectors, want)
			}
		})
	}
}

func TestOpenAIProviderNormalizesExtremeFiniteVectorsDeterministically(t *testing.T) {
	t.Parallel()

	body := `{"model":"m","data":[{"index":0,"embedding":[1e308,1e308]},` +
		`{"index":1,"embedding":[1e-300,0]}]}`
	server := responseServer(t, http.StatusOK, body)
	provider := newTestProvider(t, server.URL, 2)
	inputs := []string{"large", "small"}

	first, err := provider.Embed(context.Background(), inputs)
	if err != nil {
		t.Fatalf("first Embed() error = %v", err)
	}

	second, err := provider.Embed(context.Background(), inputs)
	if err != nil {
		t.Fatalf("second Embed() error = %v", err)
	}

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("repeated Embed() differs: %#v then %#v", first, second)
	}

	wantLarge := float32(1 / math.Sqrt(2))
	if first[0][0] != wantLarge || first[0][1] != wantLarge || first[1][0] != 1 || first[1][1] != 0 {
		t.Fatalf("Embed() = %#v, want scaled unit vectors", first)
	}
}

func validResponse() string {
	return `{"model":"m","data":[{"index":0,"embedding":[3,4]}]}`
}

func responseWithSize(t *testing.T, size int) string {
	t.Helper()

	prefix := strings.TrimSuffix(validResponse(), "}") + `,"padding":"`
	suffix := `"}`

	padding := size - len(prefix) - len(suffix)
	if padding < 0 {
		t.Fatalf("response size %d is too small", size)
	}

	body := prefix + strings.Repeat("x", padding) + suffix
	if len(body) != size {
		t.Fatalf("response fixture size = %d, want %d", len(body), size)
	}

	return body
}

func responseServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(status)
		_, _ = fmt.Fprint(writer, body)
	}))
	t.Cleanup(server.Close)

	return server
}

func newTestProvider(t *testing.T, endpoint string, dimensions int) embedding.Provider {
	t.Helper()

	provider, err := embedding.NewOpenAIProvider(embedding.OpenAIOptions{
		Endpoint:   endpoint,
		Model:      "m",
		Dimensions: dimensions,
		APIKey:     "bearer-secret",
	})
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}

	return provider
}
