//go:build integration

package embedding_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/baldaworks/boyan/internal/embedding"
)

func TestOpenAIProviderRejectsInvalidInputsBeforeNetwork(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls.Add(1)
	}))
	t.Cleanup(server.Close)

	provider, err := embedding.NewOpenAIProvider(validOptions(server.URL))
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}

	tests := []struct {
		name   string
		ctx    context.Context
		inputs []string
		code   embedding.ErrorCode
	}{
		{name: "nil context", inputs: []string{"text"}, code: embedding.CodeInvalidInput},
		{name: "empty batch", ctx: context.Background(), code: embedding.CodeInputLimit},
		{name: "batch over limit", ctx: context.Background(), inputs: make([]string, 17), code: embedding.CodeInputLimit},
		{name: "empty input", ctx: context.Background(), inputs: []string{""}, code: embedding.CodeInvalidInput},
		{name: "blank input", ctx: context.Background(), inputs: []string{" \t"}, code: embedding.CodeInvalidInput},
		{name: "invalid UTF-8", ctx: context.Background(), inputs: []string{"text\xff"}, code: embedding.CodeInvalidInput},
		{
			name:   "input over byte limit",
			ctx:    context.Background(),
			inputs: []string{strings.Repeat("я", 1025)},
			code:   embedding.CodeInputLimit,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, embedErr := provider.Embed(test.ctx, test.inputs)
			requireFailureCode(t, embedErr, test.code)
		})
	}

	if got := calls.Load(); got != 0 {
		t.Fatalf("network calls = %d, want 0", got)
	}
}

func TestOpenAIProviderEnforcesSerializedRequestLimit(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)

		var body struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)

			return
		}

		data := make([]map[string]any, len(body.Input))
		for index := range body.Input {
			data[index] = map[string]any{"index": index, "embedding": []float64{2}}
		}

		_ = json.NewEncoder(writer).Encode(map[string]any{"model": "m", "data": data})
	}))
	t.Cleanup(server.Close)

	provider, err := embedding.NewOpenAIProvider(validOptions(server.URL))
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}

	exact := inputsForRequestSize(t, 64<<10)
	if _, err := provider.Embed(context.Background(), exact); err != nil {
		t.Fatalf("Embed(exact request limit) error = %v", err)
	}

	over := inputsForRequestSize(t, (64<<10)+1)
	_, err = provider.Embed(context.Background(), over)
	requireFailureCode(t, err, embedding.CodeInputLimit)

	if got := calls.Load(); got != 1 {
		t.Fatalf("network calls = %d, want 1", got)
	}
}

func inputsForRequestSize(t *testing.T, target int) []string {
	t.Helper()

	inputs := make([]string, 16)
	for index := range inputs {
		inputs[index] = strings.Repeat("a", 2048)
	}

	payload := marshalRequestFixture(t, inputs)

	escapedBytes := target - len(payload)
	if escapedBytes < 0 || escapedBytes > 16*2048 {
		t.Fatalf("cannot construct request fixture of %d bytes from baseline %d", target, len(payload))
	}

	for inputIndex := range inputs {
		count := min(escapedBytes, 2048)
		inputs[inputIndex] = strings.Repeat("\"", count) + strings.Repeat("a", 2048-count)
		escapedBytes -= count
	}

	if got := len(marshalRequestFixture(t, inputs)); got != target {
		t.Fatalf("request fixture size = %d, want %d", got, target)
	}

	return inputs
}

func marshalRequestFixture(t *testing.T, inputs []string) []byte {
	t.Helper()

	payload, err := json.Marshal(struct {
		Model          string   `json:"model"`
		Input          []string `json:"input"`
		EncodingFormat string   `json:"encoding_format"`
	}{Model: "m", Input: inputs, EncodingFormat: "float"})
	if err != nil {
		t.Fatalf("marshal request fixture: %v", err)
	}

	return payload
}
