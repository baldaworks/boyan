package standaloneruntime

import (
	"context"
	"fmt"

	"github.com/gofrs/flock"

	"github.com/baldaworks/boyan/index/sqlite"
	projection "github.com/baldaworks/boyan/internal/projection"
	"github.com/baldaworks/boyan/pkg/memory"
)

type standaloneStorage struct {
	stateLock        *flock.Flock
	store            standaloneCanonicalStore
	reader           standaloneCanonicalReader
	index            *sqlite.Store
	applier          *sqlite.SQLiteCanonicalApplier
	projection       *projection.ProjectionRuntime
	syncer           projection.Syncer
	recallProjection memory.RecallProjection
	semantic         *semanticRuntime
}

func openStandaloneStorage(
	ctx context.Context,
	layout stateLayout,
	cfg Config,
	cleanups *standaloneCleanupStack,
) (
	*standaloneStorage,
	error,
) {
	lock, err := lockStandaloneState(layout)
	if err != nil {
		return nil, err
	}

	cleanups.Add(func() { _ = lock.Close() })

	store, reader, err := openStandaloneCanonical(ctx, layout, cfg.Canonical)
	if err != nil {
		return nil, fmt.Errorf("open Boyan canonical store: %w", err)
	}

	cleanups.Add(func() { _ = store.Close() })

	index, err := sqlite.Open(ctx, layout.Root,
		sqlite.Options{EmbeddingsEnabled: cfg.Embeddings.Provider != nil})
	if err != nil {
		return nil, fmt.Errorf("open Boyan SQLite projection: %w", err)
	}

	cleanups.Add(func() { _ = index.Close() })

	applier, err := sqlite.NewSQLiteCanonicalApplier(index, reader, store)
	if err != nil {
		return nil, err
	}

	projection, err := projection.NewProjectionRuntime(store, store, applier, boyanProjectionID, 0)
	if err != nil {
		return nil, err
	}

	storage := &standaloneStorage{
		stateLock:        lock,
		store:            store,
		reader:           reader,
		index:            index,
		applier:          applier,
		projection:       projection,
		syncer:           projection,
		recallProjection: index,
	}
	if cfg.Embeddings.Provider != nil {
		if err := storage.openSemantic(cfg.Embeddings, cleanups); err != nil {
			return nil, err
		}
	}

	return storage, nil
}
