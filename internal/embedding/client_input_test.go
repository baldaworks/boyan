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

func TestNewOpenAIProviderRejectsUnsafeOptionsWithoutDisclosure(t *testing.T) {
	t.Parallel()

	secretEndpoint := "https://operator-secret@provider.example/v1/embeddings#secret-fragment"
	secretModel := "operator-secret\nmodel"
	secretKey := "operator-secret\nheader"
	tests := []struct {
		name    string
		options embedding.OpenAIOptions
		secret  string
	}{
		{name: "empty endpoint", options: validOptions("")},
		{name: "invalid endpoint", options: validOptions("operator-secret"), secret: "operator-secret"},
		{name: "unsupported scheme", options: validOptions("file:///operator-secret"), secret: "operator-secret"},
		{name: "missing host", options: validOptions("https:///operator-secret"), secret: "operator-secret"},
		{name: "userinfo and fragment", options: validOptions(secretEndpoint), secret: secretEndpoint},
		{name: "endpoint over limit", options: validOptions("https://provider.example/" + strings.Repeat("e", 2024))},
		{name: "empty model", options: optionsWithModel("operator-secret", ""), secret: "operator-secret"},
		{name: "invalid model UTF-8", options: optionsWithModel("operator-secret", "model\xff"), secret: "operator-secret"},
		{name: "model control", options: optionsWithModel("operator-secret", secretModel), secret: "operator-secret"},
		{
			name:    "model over limit",
			options: optionsWithModel("operator-secret", strings.Repeat("m", 257)),
			secret:  "operator-secret",
		},
		{name: "zero dimensions", options: optionsWithDimensions("operator-secret", 0), secret: "operator-secret"},
		{name: "dimensions over limit", options: optionsWithDimensions("operator-secret", 4097), secret: "operator-secret"},
		{name: "key carriage return", options: optionsWithKey("operator-secret\rheader"), secret: "operator-secret"},
		{name: "key line feed", options: optionsWithKey(secretKey), secret: "operator-secret"},
		{name: "key over limit", options: optionsWithKey(strings.Repeat("s", 4097))},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			provider, err := embedding.NewOpenAIProvider(test.options)
			if provider != nil {
				t.Fatalf("NewOpenAIProvider() provider = %T, want nil", provider)
			}

			requireFailureCode(t, err, embedding.CodeInvalidInput)
			requireRedacted(t, err, test.secret, test.options.Endpoint, test.options.Model, test.options.APIKey)
		})
	}
}

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

func validOptions(endpoint string) embedding.OpenAIOptions {
	return embedding.OpenAIOptions{Endpoint: endpoint, Model: "m", Dimensions: 1}
}

func optionsWithModel(endpoint, model string) embedding.OpenAIOptions {
	options := validOptions(endpoint)
	options.Model = model

	return options
}

func optionsWithDimensions(endpoint string, dimensions int) embedding.OpenAIOptions {
	options := validOptions(endpoint)
	options.Dimensions = dimensions

	return options
}

func optionsWithKey(key string) embedding.OpenAIOptions {
	options := validOptions("https://provider.example/v1/embeddings")
	options.APIKey = key

	return options
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
