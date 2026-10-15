// Package embedding provides bounded text embedding calls.
package embedding

import "context"

// Provider converts input text into one vector per input.
type Provider interface {
	Embed(ctx context.Context, inputs []string) ([][]float32, error)
}

// OpenAIOptions describes one immutable OpenAI-compatible provider.
type OpenAIOptions struct {
	Endpoint   string
	Model      string
	Dimensions int
	APIKey     string
}
