package httpingest

import (
	"context"
	stdhttp "net/http"
	"time"

	orderedingest "github.com/baldaworks/boyan/internal/orderedingest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

const (
	defaultMaxBodyBytes   int64         = 1 << 20
	defaultRequestTimeout time.Duration = 15 * time.Second
)

// ReadyChecker reports whether the local HTTP service is currently prepared to accept work.
type ReadyChecker interface {
	CheckReadiness(ctx context.Context) error
}

// Config wires the strict versioned HTTP/JSON ingest handler.
type Config struct {
	Turns          orderedingest.TurnIngestor
	Boundaries     orderedingest.BoundaryIngestor
	Ready          ReadyChecker
	MaxBodyBytes   int64
	RequestTimeout time.Duration
}

// NewHandler constructs the strict versioned Boyan HTTP ingest surface.
func NewHandler(cfg Config) (stdhttp.Handler, error) {
	if cfg.Turns == nil || cfg.Boundaries == nil {
		return nil, memory.PermanentError(memory.CodeDisabled, "http ingest capabilities are required", nil)
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = defaultMaxBodyBytes
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = defaultRequestTimeout
	}
	mux := stdhttp.NewServeMux()
	mux.HandleFunc("/livez", livezHandler)
	mux.HandleFunc("/readyz", readyzHandler(cfg.Ready, cfg.RequestTimeout))
	mux.HandleFunc("/v1/ingest/turns", turnIngestHandler(cfg))
	mux.HandleFunc("/v1/ingest/boundaries", boundaryIngestHandler(cfg))
	return mux, nil
}
