package postgres

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"time"
)

var _ memory.CanonicalDerivedReader = (*Reader)(nil)
var _ memory.CanonicalBoundaryViewReader = (*Reader)(nil)

// Trace returns an authorized bounded provenance closure.
func (r *Reader) Trace(ctx context.Context, request memory.TraceRequest) (memory.TraceResponse, error) {
	return r.canonicalOwner().Trace(ctx, request)
}

// LoadCanonicalBoundaryView returns active compatible presentation.
func (r *Reader) LoadCanonicalBoundaryView(ctx context.Context,
	scope memory.Scope) (memory.CanonicalBoundaryView, error) {
	return r.canonicalOwner().LoadCanonicalBoundaryView(ctx, scope)
}

// LoadCanonicalBoundaryViewAt resolves context at one processing instant.
func (r *Reader) LoadCanonicalBoundaryViewAt(ctx context.Context, scope memory.Scope,
	at time.Time) (memory.CanonicalBoundaryView, error) {
	return r.canonicalOwner().LoadCanonicalBoundaryViewAt(ctx, scope, at)
}

// LoadActiveRecallRecords returns the complete bounded active view.
func (r *Reader) LoadActiveRecallRecords(ctx context.Context, scope memory.Scope,
	limit uint32) ([]memory.RecallRecord, error) {
	return r.canonicalOwner().LoadActiveRecallRecords(ctx, scope, limit)
}

// LoadCanonicalCompatibility resolves exact historical presentation metadata.
func (r *Reader) LoadCanonicalCompatibility(ctx context.Context, scope memory.Scope,
	id string) (memory.CanonicalCompatibilityPayload, bool, error) {
	return r.canonicalOwner().LoadCanonicalCompatibility(ctx, scope, id)
}
