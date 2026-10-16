package semanticprojection

import (
	"context"

	semanticstore "github.com/baldaworks/boyan/index/semantic"
	semantic "github.com/baldaworks/boyan/internal/semantic"
)

// LegacySearchReader keeps the existing in-memory recall adapter buildable
// until the SQL snapshot consumer replaces it. New adapters use SearchReader.
type LegacySearchReader interface {
	Snapshot(ctx context.Context, view semantic.View) (*semanticstore.Snapshot, bool, error)
}
