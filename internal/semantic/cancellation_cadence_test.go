package semantic_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/baldaworks/boyan/internal/semantic"
)

func TestRankCancellationCadenceAfterScanning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cancelAt int64
	}{
		{name: "during result materialization", cancelAt: 4},
		{name: "before sort", cancelAt: 5},
		{name: "after sort", cancelAt: 6},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx := newCountingCancelContext(test.cancelAt)
			_, err := semantic.Rank(
				ctx,
				[][]float32{{1}},
				[]semantic.Chunk{{RevisionID: "revision", Vector: []float32{1}}},
				1,
			)

			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Rank() error = %v, want context.Canceled", err)
			}
		})
	}
}

func TestFuseCancellationCadenceAfterScanning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cancelAt int64
	}{
		{name: "during result materialization", cancelAt: 3},
		{name: "before sort", cancelAt: 4},
		{name: "after sort", cancelAt: 5},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx := newCountingCancelContext(test.cancelAt)
			_, err := semantic.Fuse(ctx, []string{"revision"}, nil, 1)

			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Fuse() error = %v, want context.Canceled", err)
			}
		})
	}
}

type countingCancelContext struct {
	context.Context

	cancelAt int64
	checks   atomic.Int64
	done     chan struct{}
	once     sync.Once
}

func newCountingCancelContext(cancelAt int64) *countingCancelContext {
	return &countingCancelContext{
		Context:  context.Background(),
		cancelAt: cancelAt,
		done:     make(chan struct{}),
	}
}

func (ctx *countingCancelContext) Done() <-chan struct{} {
	return ctx.done
}

func (ctx *countingCancelContext) Err() error {
	if ctx.checks.Add(1) < ctx.cancelAt {
		return nil
	}

	ctx.once.Do(func() { close(ctx.done) })

	return context.Canceled
}
