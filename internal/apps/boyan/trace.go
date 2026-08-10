package app

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// TraceService adapts the read-only canonical derived reader to the portable
// TraceReader capability.  It never accepts a scope from a transport adapter;
// the caller must have resolved the exact scope before constructing the
// request.
type TraceService struct {
	reader memory.CanonicalDerivedReader
}

// NewTraceService constructs a bounded trace adapter.
func NewTraceService(reader memory.CanonicalDerivedReader) (*TraceService, error) {
	if reader == nil {
		return nil, memory.PermanentError(memory.CodeStoreFailure, "trace reader is required", nil)
	}
	return &TraceService{reader: reader}, nil
}

// Trace implements TraceReader.
func (s *TraceService) Trace(ctx context.Context, request memory.TraceRequest) (memory.TraceResponse, error) {
	if s == nil || s.reader == nil {
		return memory.TraceResponse{}, memory.PermanentError(memory.CodeDisabled, "trace service is unavailable", nil)
	}
	normalized, err := memory.NormalizeTraceRequest(request, memory.MaxTraceNodes)
	if err != nil {
		return memory.TraceResponse{}, err
	}
	return s.reader.Trace(ctx, normalized)
}

var _ TraceReader = (*TraceService)(nil)
