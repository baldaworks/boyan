package main

import (
	"context"
	"fmt"
	stdhttp "net/http"

	boyanapp "github.com/baldaworks/boyan/internal/apps/boyan"
	"github.com/baldaworks/boyan/internal/httpingest"
	"github.com/baldaworks/boyan/mcp"
	"github.com/baldaworks/boyan/store/badger"
	"github.com/rs/zerolog"
)

func newStandaloneHTTPServer(cfg applicationConfig, logger zerolog.Logger, maintenance *badger.CanonicalMaintenance, sequenced *boyanapp.SequencedIngestRuntime, recall *boyanapp.RecallService, trace *boyanapp.TraceService, forget *boyanapp.ForgetService) (*stdhttp.Server, error) {
	if err := httpingest.ValidateLoopbackAddress(cfg.HTTP.Listen); err != nil {
		return nil, err
	}
	ingestHandler, err := httpingest.NewHandler(httpingest.Config{
		Turns:          sequenced,
		Boundaries:     sequenced,
		Ready:          readyCheckerFunc(func(ctx context.Context) error { return checkServeReadiness(ctx, maintenance) }),
		MaxBodyBytes:   cfg.HTTP.MaxBodyBytes,
		RequestTimeout: cfg.HTTP.RequestTimeout,
	})
	if err != nil {
		return nil, err
	}
	mcpHandler, err := mcp.NewHandler(mcp.Config{
		Enabled:   true,
		Searcher:  recall,
		Tracer:    trace,
		Forgetter: forget,
	})
	if err != nil {
		return nil, err
	}
	rootMux := stdhttp.NewServeMux()
	rootMux.Handle("/mcp", loggingMiddleware(logger, mcpHandler))
	rootMux.Handle("/", loggingMiddleware(logger, ingestHandler))

	return &stdhttp.Server{
		Addr:              cfg.HTTP.Listen,
		Handler:           rootMux,
		ReadHeaderTimeout: cfg.HTTP.RequestTimeout,
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
