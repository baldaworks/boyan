package memoryread

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// RecallReader exposes bounded, exact-scope canonical recall.
type RecallReader interface {
	Search(ctx context.Context, request memory.RecallRequest) (memory.RecallResponse, error)
}

// TraceReader exposes bounded provenance inspection for one exact scope.
type TraceReader interface {
	Trace(ctx context.Context, request memory.TraceRequest) (memory.TraceResponse, error)
}
