package sqlite

import (
	"context"
	"database/sql"

	"github.com/baldaworks/boyan/pkg/memory"
)

// Claims are nonblocking: release reserves before waiting for its scope, while
// Stage claims after taking its scope. Neither introduces an inverse wait edge.
type semanticOperation uint8

const (
	semanticOperationStage semanticOperation = iota + 1
	semanticOperationRelease
)

// Semantic writes and canonical forgetting share the existing scope lease.
// Lock order is scope then writer transaction; provider work happens earlier.
func (store *Store) semanticScopeLease(ctx context.Context, scope memory.Scope) (context.Context, func(), error) {
	operation, done, err := store.acquire(ctx)
	if err != nil {
		return nil, nil, err
	}

	unlock, err := store.lockScope(operation, scope)
	if err != nil {
		done()

		return nil, nil, err
	}

	return operation, func() { unlock(); done() }, nil
}

func (store *Store) semanticScopeWrite(ctx context.Context, scope memory.Scope,
	apply func(context.Context, *sql.Tx) error) error {
	operation, done, err := store.semanticScopeLease(ctx, scope)
	if err != nil {
		return err
	}
	defer done()

	return store.write(operation, apply)
}
