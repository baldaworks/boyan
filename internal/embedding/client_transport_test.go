package embedding_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/baldaworks/boyan/internal/embedding"
)

func TestOpenAIProviderGivesCallerCancellationPrecedenceOverValidation(t *testing.T) {
	t.Parallel()

	provider, err := embedding.NewOpenAIProvider(validOptions("https://provider.example/v1/embeddings"))
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}

	ctx := newCancelAfterFirstCheckContext()

	_, err = provider.Embed(ctx, []string{""})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Embed() error = %v, want context.Canceled", err)
	}
}

type cancelAfterFirstCheckContext struct {
	context.Context

	done chan struct{}
	once sync.Once
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func newCancelAfterFirstCheckContext() *cancelAfterFirstCheckContext {
	return &cancelAfterFirstCheckContext{Context: context.Background(), done: make(chan struct{})}
}

func (ctx *cancelAfterFirstCheckContext) Done() <-chan struct{} {
	return ctx.done
}

func (ctx *cancelAfterFirstCheckContext) Err() error {
	first := false

	ctx.once.Do(func() {
		first = true

		close(ctx.done)
	})

	if first {
		return nil
	}

	return context.Canceled
}
