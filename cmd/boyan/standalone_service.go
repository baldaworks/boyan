package main

import (
	"context"
	"fmt"
	stdhttp "net/http"
	"os"
	"strings"
	"sync"

	"github.com/rs/zerolog"

	"github.com/baldaworks/boyan/index/bleve"
	boyanapp "github.com/baldaworks/boyan/internal/apps/boyan"
	"github.com/baldaworks/boyan/internal/derivation"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/badger"
)

type standaloneService struct {
	cfg         applicationConfig
	logger      zerolog.Logger
	state       stateLayout
	store       *badger.CanonicalStore
	maintenance *badger.CanonicalMaintenance
	reader      *badger.CanonicalReader
	applier     *bleve.BleveCanonicalApplier
	projection  *boyanapp.ProjectionRuntime
	sequenced   *boyanapp.SequencedIngestRuntime
	invoker     *derivation.Invoker
	recall      *boyanapp.RecallService
	trace       *boyanapp.TraceService
	forget      *boyanapp.ForgetService
	server      *stdhttp.Server

	mu      sync.Mutex
	started bool
	closing bool
	serving error
	done    chan error
}

type standaloneCleanupStack struct {
	funcs []func()
}

func (s *standaloneCleanupStack) Add(fn func()) {
	if fn != nil {
		s.funcs = append(s.funcs, fn)
	}
}

func (s *standaloneCleanupStack) Close() {
	for index := len(s.funcs) - 1; index >= 0; index-- {
		s.funcs[index]()
	}
	s.funcs = nil
}

func newStandaloneService(cfg applicationConfig, logger zerolog.Logger) (_ *standaloneService, err error) {
	layout, err := buildStateLayout(cfg.StateDir)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.Derivation.Provider) == "" {
		return nil, fmt.Errorf("Boyan derivation provider is required; configure derivation.provider")
	}
	workingDir := strings.TrimSpace(cfg.Derivation.WorkingDir)
	if workingDir == "" {
		workingDir = "."
	}
	if err := ensureStandaloneState(layout); err != nil {
		return nil, err
	}

	cleanups := standaloneCleanupStack{}
	defer func() {
		if err != nil {
			cleanups.Close()
		}
	}()

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
	projectionIndex, err := bleve.Open(layout.ProjectionDir)
	if err != nil {
		return nil, fmt.Errorf("open Boyan Bleve projection: %w", err)
	}
	cleanups.Add(func() { _ = projectionIndex.Close() })

	applier, err := bleve.NewBleveCanonicalApplier(projectionIndex, reader)
	if err != nil {
		return nil, err
	}
	cleanups.Add(func() { _ = applier.Close() })

	projectionRuntime, err := boyanapp.NewProjectionRuntime(store, store, applier, boyanProjectionID, 0)
	if err != nil {
		return nil, err
	}

	invoker, err := derivation.NewInvoker(derivation.Config{
		Runtime:        cfg.Derivation.Runtime,
		ProviderID:     cfg.Derivation.Provider,
		WorkingDir:     workingDir,
		MaxOutputBytes: cfg.Derivation.MaxOutputBytes,
		Timeout:        cfg.Derivation.Timeout,
	})
	if err != nil {
		return nil, err
	}
	cleanups.Add(func() { _ = invoker.Close(context.Background()) })

	deriver, err := boyanapp.NewDeriver(invoker)
	if err != nil {
		return nil, err
	}
	turnProcessor, err := boyanapp.NewTurnProcessor(store, deriver, memory.PolicyRegistry{Version: "policy-v1"}, memory.LegacyDerivationRef(), projectionRuntime)
	if err != nil {
		return nil, err
	}
	boundaryStore, err := newCanonicalBoundaryStore(store, reader)
	if err != nil {
		return nil, err
	}
	boundaryProcessor, err := boyanapp.NewBoundaryProcessor(boundaryStore, deriver, deriver, memory.LegacyDerivationRef())
	if err != nil {
		return nil, err
	}
	boundaryService, err := boyanapp.NewBoundaryService(boundaryProcessor, projectionRuntime)
	if err != nil {
		return nil, err
	}
	sequencedRuntime, err := boyanapp.NewSequencedIngestRuntime(store, store, turnProcessor, boundaryService, boyanapp.SequencedIngestConfig{})
	if err != nil {
		return nil, err
	}
	recall, err := boyanapp.NewRecallService(reader, projectionIndex)
	if err != nil {
		return nil, err
	}
	trace, err := boyanapp.NewTraceService(reader)
	if err != nil {
		return nil, err
	}
	forgetOperations, err := newCanonicalForgetAdapter(store)
	if err != nil {
		return nil, err
	}
	forget, err := boyanapp.NewForgetServiceWithProjection(forgetOperations, projectionRuntime)
	if err != nil {
		return nil, err
	}
	server, err := newStandaloneHTTPServer(cfg, logger, maintenance, sequencedRuntime, recall, trace, forget)
	if err != nil {
		return nil, err
	}

	return &standaloneService{
		cfg:         cfg,
		logger:      logger,
		state:       layout,
		store:       store,
		maintenance: maintenance,
		reader:      reader,
		applier:     applier,
		projection:  projectionRuntime,
		sequenced:   sequencedRuntime,
		invoker:     invoker,
		recall:      recall,
		trace:       trace,
		forget:      forget,
		server:      server,
		done:        make(chan error, 1),
	}, nil
}

func ensureStandaloneState(layout stateLayout) error {
	if err := os.MkdirAll(layout.Root, 0o700); err != nil {
		return fmt.Errorf("create Boyan state root: %w", err)
	}
	for _, directory := range []string{layout.CanonicalDir, layout.ProjectionDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("create Boyan state directory %s: %w", directory, err)
		}
	}
	return nil
}
