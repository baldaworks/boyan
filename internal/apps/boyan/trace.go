package app

import (
	"context"

	"github.com/baldaworks/boyan"
)

// TraceService adapts the read-only canonical derived reader to the portable
// TraceReader capability.  It never accepts a scope from a transport adapter;
// the caller must have resolved the exact scope before constructing the
// request.
type TraceService struct {
	reader boyan.CanonicalDerivedReader
}

// NewTraceService constructs a bounded trace adapter.
func NewTraceService(reader boyan.CanonicalDerivedReader) (*TraceService, error) {
	if reader == nil {
		return nil, boyan.PermanentError(boyan.CodeStoreFailure, "trace reader is required", nil)
	}
	return &TraceService{reader: reader}, nil
}

// Trace implements TraceReader.
func (s *TraceService) Trace(ctx context.Context, request boyan.TraceRequest) (boyan.TraceResponse, error) {
	if s == nil || s.reader == nil {
		return boyan.TraceResponse{}, boyan.PermanentError(boyan.CodeDisabled, "trace service is unavailable", nil)
	}
	normalized, err := boyan.NormalizeTraceRequest(request, boyan.MaxTraceNodes)
	if err != nil {
		return boyan.TraceResponse{}, err
	}
	return s.reader.Trace(ctx, normalized)
}

var _ TraceReader = (*TraceService)(nil)
