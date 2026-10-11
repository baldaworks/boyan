package standaloneruntime

import (
	"fmt"

	"github.com/baldaworks/boyan/index/bleve"
	projection "github.com/baldaworks/boyan/internal/projection"
	"github.com/baldaworks/boyan/store/badger"
)

type standaloneStorage struct {
	store       *badger.CanonicalStore
	maintenance *badger.CanonicalMaintenance
	reader      *badger.CanonicalReader
	index       *bleve.BleveRecallProjection
	applier     *bleve.BleveCanonicalApplier
	projection  *projection.ProjectionRuntime
}

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

	index, err := bleve.Open(layout.ProjectionDir)
	if err != nil {
		return nil, fmt.Errorf("open Boyan Bleve projection: %w", err)
	}

	cleanups.Add(func() { _ = index.Close() })

	applier, err := bleve.NewBleveCanonicalApplier(index, reader)
	if err != nil {
		return nil, err
	}

	cleanups.Add(func() { _ = applier.Close() })

	projection, err := projection.NewProjectionRuntime(store, store, applier, boyanProjectionID, 0)
	if err != nil {
		return nil, err
	}

	return &standaloneStorage{
		store:       store,
		maintenance: maintenance,
		reader:      reader,
		index:       index,
		applier:     applier,
		projection:  projection,
	}, nil
}
