package boyan_test

import boyan "github.com/baldaworks/boyan/pkg/boyan"

import (
	"context"
	"net/http"
	"testing"

	"github.com/baldaworks/boyan/mcp"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

type publicHostContract interface {
	publicHostLifecycle
	publicHostTransport
	publicHostCapabilities
}

type publicHostLifecycle interface {
	Start(ctx context.Context) error
	Run(ctx context.Context) error
	Wait(ctx context.Context) error
	Stop(ctx context.Context) error
	Close() error
}

type publicHostTransport interface {
	Ready() bool
	Addr() string
	Handler() http.Handler
	MCP() *mcp.Service
}

type publicHostCapabilities interface {
	IngestSequencedTurn(ctx context.Context, request memory.SequencedTurn) (memory.IngestOutcome, error)
	ApplySequencedBoundary(ctx context.Context, request memory.SequencedBoundary) (memory.IngestOutcome, error)
	Search(ctx context.Context, request memory.RecallRequest) (memory.RecallResponse, error)
	Trace(ctx context.Context, request memory.TraceRequest) (memory.TraceResponse, error)
	ForgetSource(ctx context.Context, command memory.ForgetSourceCommand) (memory.ForgetOutcome, error)
	ForgetScope(ctx context.Context, command memory.ForgetScopeCommand) (memory.ForgetOutcome, error)
}

func TestHostPublicContractCompiles(t *testing.T) {
	t.Parallel()

	var _ publicHostContract = (*boyan.Host)(nil)
}
