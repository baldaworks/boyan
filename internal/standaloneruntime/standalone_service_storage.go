package standaloneruntime

import (
	"context"
	"fmt"

	"github.com/baldaworks/boyan/index/sqlite"
	projection "github.com/baldaworks/boyan/internal/projection"
	"github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/badger"
)

type standaloneStorage struct {
	store            *badger.CanonicalStore
	maintenance      *badger.CanonicalMaintenance
	reader           *badger.CanonicalReader
	index            *sqlite.Store
	applier          *sqlite.SQLiteCanonicalApplier
	projection       *projection.ProjectionRuntime
	syncer           projection.Syncer
	recallProjection memory.RecallProjection
	semantic         *semanticRuntime
}

//nolint:funlen // Required lexical ownership precedes the isolated optional graph.
func openStandaloneStorage(
	layout stateLayout,
	cfg Config,
	cleanups *standaloneCleanupStack,
) (
	*standaloneStorage,
	error,
) {
	store, err := badger.OpenCanonicalStore(layout.CanonicalDir)
	if err != nil {
		return nil, fmt.Errorf("open Boyan canonical store: %w", err)
	}

	cleanups.Add(func() { _ = store.Close() })

	maintenance, err := badger.NewCanonicalMaintenance(store, badger.CanonicalMaintenanceConfig{
		ValueLogGCInterval: cfg.Maintenance.ValueLogGCInterval,
		DiscardRatio:       cfg.Maintenance.DiscardRatio,
	})
	if err != nil {
		return nil, err
	}

	reader, err := badger.NewCanonicalReader(store)
	if err != nil {
		return nil, err
	}

	index, err := sqlite.Open(context.Background(), layout.Root,
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
		store:            store,
		maintenance:      maintenance,
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
