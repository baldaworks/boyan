package postgres

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// PrepareSourcePrivacy reconstructs bounded privacy authority before readiness.
func (s *Store) PrepareSourcePrivacy(ctx context.Context) error {
	return s.canonicalOwner().PrepareSourcePrivacy(ctx)
}

// SourcePrivacyReady checks durable privacy capability and the loading fence.
func (s *Store) SourcePrivacyReady(ctx context.Context) error {
	return s.canonicalOwner().SourcePrivacyReady(ctx)
}

// PutPayload persists immutable content subject to erase locks and tombstones.
func (s *Store) PutPayload(ctx context.Context, data []byte, ref memory.PayloadRef) error {
	return s.canonicalOwner().PutPayload(ctx, data, ref)
}

// LoadPayload validates and loads the exact referenced immutable content.
func (s *Store) LoadPayload(ctx context.Context, ref memory.PayloadRef) ([]byte, error) {
	return s.canonicalOwner().LoadPayload(ctx, ref)
}

// DeletePayload removes raw content after logical forgetting.
func (s *Store) DeletePayload(ctx context.Context, ref memory.PayloadRef) error {
	return s.canonicalOwner().DeletePayload(ctx, ref)
}

// IsSourceDenied checks exact-scope durable source denial.
func (s *Store) IsSourceDenied(ctx context.Context, scope memory.Scope, id string) (bool, error) {
	return s.canonicalOwner().IsSourceDenied(ctx, scope, id)
}

// IsRevisionDenied checks exact-scope durable revision denial.
func (s *Store) IsRevisionDenied(ctx context.Context, scope memory.Scope, id string) (bool, error) {
	return s.canonicalOwner().IsRevisionDenied(ctx, scope, id)
}
