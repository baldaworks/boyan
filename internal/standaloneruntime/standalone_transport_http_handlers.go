package standaloneruntime

import (
	stdhttp "net/http"

	"github.com/baldaworks/boyan/internal/httpingest"
	orderedingest "github.com/baldaworks/boyan/internal/orderedingest"
	"github.com/baldaworks/boyan/mcp"
)

func newHTTPIngestHandler(
	cfg standaloneHTTPConfig,
	reporter readinessErrorReporter,
	turns orderedingest.TurnIngestor,
	boundaries orderedingest.BoundaryIngestor,
) (stdhttp.Handler, error) {
	return httpingest.NewHandler(httpingest.Config{
		Turns:          turns,
		Boundaries:     boundaries,
		Ready:          readinessChecker{reporter: reporter},
		MaxBodyBytes:   cfg.MaxBodyBytes,
		RequestTimeout: cfg.RequestTimeout,
	})
}

func newHTTPMCPHandler(searcher mcp.Searcher, tracer mcp.Tracer, forgetter mcp.Forgetter) (stdhttp.Handler, error) {
	return mcp.NewHandler(mcp.Config{
		Enabled:   true,
		Searcher:  searcher,
		Tracer:    tracer,
		Forgetter: forgetter,
	})
}
