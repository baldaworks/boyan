package mcp

import (
	"context"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Searcher is the bounded canonical recall port. Implementations own
// canonical hydration; this adapter only validates the result envelope.
type Searcher interface {
	Search(ctx context.Context, req memory.RecallRequest) (memory.RecallResponse, error)
}

// Tracer is the bounded canonical provenance port.
type Tracer interface {
	Trace(ctx context.Context, req memory.TraceRequest) (memory.TraceResponse, error)
}

// Forgetter is the explicit canonical forgetting port.
type Forgetter interface {
	ForgetSource(ctx context.Context, command memory.ForgetSourceCommand) (memory.ForgetOutcome, error)
	ForgetScope(ctx context.Context, command memory.ForgetScopeCommand) (memory.ForgetOutcome, error)
}

// Config configures the standalone Boyan MCP service.
type Config struct {
	Enabled           bool
	Searcher          Searcher
	Tracer            Tracer
	Forgetter         Forgetter
	Timeout           time.Duration
	Now               func() time.Time
	Implementation    *sdkmcp.Implementation
	ServerOptions     *sdkmcp.ServerOptions
	StreamableOptions *sdkmcp.StreamableHTTPOptions
}

// Service handles the Boyan MCP operations.
type Service struct {
	enabled   bool
	searcher  Searcher
	tracer    Tracer
	forgetter Forgetter
	timeout   time.Duration
	now       func() time.Time
}
