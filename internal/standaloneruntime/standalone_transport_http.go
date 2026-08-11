package standaloneruntime

import (
	stdhttp "net/http"
	"time"

	"github.com/baldaworks/boyan/internal/httpingest"
	orderedingest "github.com/baldaworks/boyan/internal/orderedingest"
	"github.com/baldaworks/boyan/mcp"
	"github.com/rs/zerolog"
)

type standaloneHTTPConfig struct {
	Listen         string
	RequestTimeout time.Duration
	MaxBodyBytes   int64
}

type readinessErrorReporter interface {
	LastError() error
}

func newHTTPServer(
	cfg standaloneHTTPConfig,
	logger zerolog.Logger,
	reporter readinessErrorReporter,
	turns orderedingest.TurnIngestor,
	boundaries orderedingest.BoundaryIngestor,
	searcher mcp.Searcher,
	tracer mcp.Tracer,
	forgetter mcp.Forgetter,
) (*stdhttp.Server, error) {
	if err := httpingest.ValidateLoopbackAddress(cfg.Listen); err != nil {
		return nil, err
	}
	ingestHandler, err := newHTTPIngestHandler(cfg, reporter, turns, boundaries)
	if err != nil {
		return nil, err
	}
	mcpHandler, err := newHTTPMCPHandler(searcher, tracer, forgetter)
	if err != nil {
		return nil, err
	}
	rootMux := stdhttp.NewServeMux()
	rootMux.Handle("/mcp", httpLoggingMiddleware(logger, mcpHandler))
	rootMux.Handle("/", httpLoggingMiddleware(logger, ingestHandler))

	return &stdhttp.Server{
		Addr:              cfg.Listen,
		Handler:           rootMux,
		ReadHeaderTimeout: cfg.RequestTimeout,
	}, nil
}
