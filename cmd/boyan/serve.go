package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	stdhttp "net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"go.uber.org/fx"

	"github.com/baldaworks/boyan"
	boyanapp "github.com/baldaworks/boyan/internal/apps/boyan"
	"github.com/baldaworks/boyan/index/bleve"
	"github.com/baldaworks/boyan/internal/derivation"
	"github.com/baldaworks/boyan/internal/httpingest"
	"github.com/baldaworks/boyan/mcp"
	"github.com/baldaworks/boyan/store/badger"
)

type stateLayout struct {
	Root          string
	CanonicalDir  string
	ProjectionDir string
}

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

func newServeCommand(configPath *string) *cobra.Command {
	command := &cobra.Command{
		Use:   "serve",
		Short: "Run the standalone Boyan local service.",
		Long: strings.TrimSpace(`
Run the standalone Boyan service on one loopback HTTP listener.

The listener serves:
  - /livez
  - /readyz
  - /v1/ingest/turns
  - /v1/ingest/boundaries
  - /mcp
`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadApplicationConfig(derefString(configPath), cmd.Flags())
			if err != nil {
				return err
			}
			return runServe(cmd.Context(), cfg, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	bindServeFlags(command.Flags())
	return command
}

func runServe(ctx context.Context, cfg applicationConfig, _ io.Writer, _ io.Writer) error {
	logger, err := newLogger(cfg.Log)
	if err != nil {
		return err
	}
	service, err := newStandaloneService(cfg, logger)
	if err != nil {
		return err
	}
	fxApp := fx.New(
		fx.Supply(service),
		fx.Supply(logger),
		fx.Invoke(func(lifecycle fx.Lifecycle, svc *standaloneService) {
			lifecycle.Append(fx.Hook{
				OnStart: svc.Start,
				OnStop:  svc.Stop,
			})
		}),
	)
	startCtx, cancel := context.WithTimeout(ctx, cfg.HTTP.ShutdownTimeout)
	defer cancel()
	if err := fxApp.Start(startCtx); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
	case err := <-service.done:
		if err != nil && !errors.Is(err, stdhttp.ErrServerClosed) {
			stopCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
			defer cancel()
			_ = fxApp.Stop(stopCtx)
			return err
		}
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer cancel()
	return fxApp.Stop(stopCtx)
}

func newStandaloneService(cfg applicationConfig, logger zerolog.Logger) (*standaloneService, error) {
	layout, err := buildStateLayout(cfg.StateDir)
	if err != nil {
		return nil, err
	}
	if err := httpingest.ValidateLoopbackAddress(cfg.HTTP.Listen); err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.Derivation.Provider) == "" {
		return nil, fmt.Errorf("Boyan derivation provider is required; configure derivation.provider")
	}
	workingDir := strings.TrimSpace(cfg.Derivation.WorkingDir)
	if workingDir == "" {
		workingDir = "."
	}
	if err := os.MkdirAll(layout.Root, 0o700); err != nil {
		return nil, fmt.Errorf("create Boyan state root: %w", err)
	}
	for _, directory := range []string{layout.CanonicalDir, layout.ProjectionDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return nil, fmt.Errorf("create Boyan state directory %s: %w", directory, err)
		}
	}

	store, err := badger.OpenCanonicalStore(layout.CanonicalDir)
	if err != nil {
		return nil, fmt.Errorf("open Boyan canonical store: %w", err)
	}
	maintenance, err := badger.NewCanonicalMaintenance(store, badger.CanonicalMaintenanceConfig{
		ValueLogGCInterval: cfg.Maintenance.ValueLogGCInterval,
		DiscardRatio:       cfg.Maintenance.DiscardRatio,
	})
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	reader, err := badger.NewCanonicalReader(store)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	projectionIndex, err := bleve.Open(layout.ProjectionDir)
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("open Boyan Bleve projection: %w", err)
	}
	applier, err := bleve.NewBleveCanonicalApplier(projectionIndex, reader)
	if err != nil {
		_ = projectionIndex.Close()
		_ = store.Close()
		return nil, err
	}
	projectionRuntime, err := boyanapp.NewProjectionRuntime(store, store, applier, boyanProjectionID, 0)
	if err != nil {
		_ = applier.Close()
		_ = store.Close()
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
		_ = applier.Close()
		_ = store.Close()
		return nil, err
	}
	deriver, err := boyanapp.NewDeriver(invoker)
	if err != nil {
		_ = invoker.Close(context.Background())
		_ = applier.Close()
		_ = store.Close()
		return nil, err
	}
	turnProcessor, err := boyanapp.NewTurnProcessor(store, deriver, boyan.PolicyRegistry{Version: "policy-v1"}, boyan.LegacyDerivationRef(), projectionRuntime)
	if err != nil {
		_ = invoker.Close(context.Background())
		_ = applier.Close()
		_ = store.Close()
		return nil, err
	}
	boundaryStore, err := newCanonicalBoundaryStore(store, reader)
	if err != nil {
		_ = invoker.Close(context.Background())
		_ = applier.Close()
		_ = store.Close()
		return nil, err
	}
	boundaryProcessor, err := boyanapp.NewBoundaryProcessor(boundaryStore, deriver, deriver, boyan.LegacyDerivationRef())
	if err != nil {
		_ = invoker.Close(context.Background())
		_ = applier.Close()
		_ = store.Close()
		return nil, err
	}
	boundaryService, err := boyanapp.NewBoundaryService(boundaryProcessor, projectionRuntime)
	if err != nil {
		_ = invoker.Close(context.Background())
		_ = applier.Close()
		_ = store.Close()
		return nil, err
	}
	sequencedRuntime, err := boyanapp.NewSequencedIngestRuntime(store, store, turnProcessor, boundaryService, boyanapp.SequencedIngestConfig{})
	if err != nil {
		_ = invoker.Close(context.Background())
		_ = applier.Close()
		_ = store.Close()
		return nil, err
	}
	recall, err := boyanapp.NewRecallService(reader, projectionIndex)
	if err != nil {
		_ = invoker.Close(context.Background())
		_ = applier.Close()
		_ = store.Close()
		return nil, err
	}
	trace, err := boyanapp.NewTraceService(reader)
	if err != nil {
		_ = invoker.Close(context.Background())
		_ = applier.Close()
		_ = store.Close()
		return nil, err
	}
	forgetOperations, err := newCanonicalForgetAdapter(store)
	if err != nil {
		_ = invoker.Close(context.Background())
		_ = applier.Close()
		_ = store.Close()
		return nil, err
	}
	forget, err := boyanapp.NewForgetServiceWithProjection(forgetOperations, projectionRuntime)
	if err != nil {
		_ = invoker.Close(context.Background())
		_ = applier.Close()
		_ = store.Close()
		return nil, err
	}
	ingestHandler, err := httpingest.NewHandler(httpingest.Config{
		Turns:          sequencedRuntime,
		Boundaries:     sequencedRuntime,
		Ready:          readyCheckerFunc(func(ctx context.Context) error { return checkServeReadiness(ctx, maintenance) }),
		MaxBodyBytes:   cfg.HTTP.MaxBodyBytes,
		RequestTimeout: cfg.HTTP.RequestTimeout,
	})
	if err != nil {
		_ = invoker.Close(context.Background())
		_ = applier.Close()
		_ = store.Close()
		return nil, err
	}
	mcpHandler, err := mcp.NewHandler(mcp.Config{
		Enabled:   true,
		Searcher:  recall,
		Tracer:    trace,
		Forgetter: forget,
	})
	if err != nil {
		_ = invoker.Close(context.Background())
		_ = applier.Close()
		_ = store.Close()
		return nil, err
	}
	rootMux := stdhttp.NewServeMux()
	rootMux.Handle("/mcp", loggingMiddleware(logger, mcpHandler))
	rootMux.Handle("/", loggingMiddleware(logger, ingestHandler))

	server := &stdhttp.Server{
		Addr:              cfg.HTTP.Listen,
		Handler:           rootMux,
		ReadHeaderTimeout: cfg.HTTP.RequestTimeout,
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

func (s *standaloneService) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil
	}
	s.started = true
	s.mu.Unlock()

	if err := s.maintenance.Start(ctx); err != nil {
		return err
	}
	if err := s.projection.Start(ctx); err != nil {
		_ = s.maintenance.Close(context.Background())
		return err
	}
	if err := s.sequenced.Start(ctx); err != nil {
		_ = s.projection.Close(context.Background())
		_ = s.maintenance.Close(context.Background())
		return err
	}
	listener, err := net.Listen("tcp", s.cfg.HTTP.Listen)
	if err != nil {
		_ = s.sequenced.Close(context.Background())
		_ = s.projection.Close(context.Background())
		_ = s.maintenance.Close(context.Background())
		return fmt.Errorf("listen on %s: %w", s.cfg.HTTP.Listen, err)
	}
	s.server.Addr = listener.Addr().String()
	s.logger.Info().
		Str("listen", listener.Addr().String()).
		Str("state_dir", s.state.Root).
		Msg("Boyan service starting")
	go func() {
		err := s.server.Serve(listener)
		s.mu.Lock()
		s.serving = err
		s.mu.Unlock()
		s.done <- err
	}()
	return nil
}

func (s *standaloneService) Stop(ctx context.Context) error {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return nil
	}
	s.closing = true
	s.mu.Unlock()

	s.logger.Info().Msg("Boyan service stopping")
	var firstErr error
	if err := s.server.Shutdown(ctx); err != nil && !errors.Is(err, stdhttp.ErrServerClosed) && firstErr == nil {
		firstErr = err
	}
	if err := s.sequenced.Close(ctx); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := s.projection.Close(ctx); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := s.invoker.Close(ctx); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := s.applier.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := s.maintenance.Close(ctx); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func (s *standaloneService) address() string {
	if s == nil || s.server == nil {
		return ""
	}
	return s.server.Addr
}

func buildStateLayout(root string) (stateLayout, error) {
	trimmed := strings.TrimSpace(root)
	if trimmed == "" {
		return stateLayout{}, fmt.Errorf("Boyan state directory is required")
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return stateLayout{}, fmt.Errorf("resolve Boyan state directory: %w", err)
	}
	clean := filepath.Clean(abs)
	return stateLayout{
		Root:          clean,
		CanonicalDir:  filepath.Join(clean, boyanCanonicalDirName),
		ProjectionDir: filepath.Join(clean, boyanProjectionDirName),
	}, nil
}

func checkServeReadiness(ctx context.Context, maintenance *badger.CanonicalMaintenance) error {
	if ctx == nil {
		return fmt.Errorf("readiness context is required")
	}
	if maintenance != nil {
		if err := maintenance.LastError(); err != nil {
			return err
		}
	}
	return nil
}

type readyCheckerFunc func(context.Context) error

func (f readyCheckerFunc) CheckReadiness(ctx context.Context) error { return f(ctx) }

func newLogger(cfg logConfig) (zerolog.Logger, error) {
	level := zerolog.InfoLevel
	if strings.TrimSpace(cfg.Level) != "" {
		parsed, err := zerolog.ParseLevel(strings.TrimSpace(cfg.Level))
		if err != nil {
			return zerolog.Logger{}, fmt.Errorf("parse Boyan log level: %w", err)
		}
		level = parsed
	}
	logger := zerolog.New(os.Stderr).With().Timestamp().Str("service", "boyan").Logger().Level(level)
	return logger, nil
}

func loggingMiddleware(logger zerolog.Logger, next stdhttp.Handler) stdhttp.Handler {
	return stdhttp.HandlerFunc(func(writer stdhttp.ResponseWriter, request *stdhttp.Request) {
		started := time.Now().UTC()
		recorder := &statusRecorder{ResponseWriter: writer, status: stdhttp.StatusOK}
		next.ServeHTTP(recorder, request)
		logger.Info().
			Str("method", request.Method).
			Str("path", request.URL.Path).
			Int("status", recorder.status).
			Dur("elapsed", time.Since(started)).
			Msg("Boyan request")
	})
}

type statusRecorder struct {
	stdhttp.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
