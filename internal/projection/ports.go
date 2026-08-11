package projection

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Syncer advances a rebuildable projection after a canonical commit.
type Syncer interface {
	Sync(ctx context.Context, scope memory.Scope) error
}

// Lifecycle starts and closes projection runtime workers.
type Lifecycle interface {
	Start(ctx context.Context) error
	Close(ctx context.Context) error
}
