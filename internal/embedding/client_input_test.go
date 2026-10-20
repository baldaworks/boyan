package embedding_test

import (
	"strings"
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
