package standaloneruntime

import (
	"context"
	"fmt"
	stdhttp "net/http"

	"github.com/baldaworks/boyan/mcp"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Runtime is the narrow standalone runtime seam used by the public Boyan host.
type Runtime struct {
	service *standaloneService
	handler stdhttp.Handler
	mcp     *mcp.Service
}

// New constructs one standalone Boyan runtime without running its process loop.
func New(cfg Config) (*Runtime, error) {
	logger, err := newLogger(cfg)
	if err != nil {
		return nil, err
	}

	service, err := newStandaloneService(cfg, logger)
	if err != nil {
		return nil, err
	}

	runtime := &Runtime{service: service, handler: service.server.Handler}
	runtime.mcp = mcp.New(mcp.Config{Enabled: true, Searcher: runtime, Tracer: runtime, Forgetter: runtime})

	return runtime, nil
}

func Run(ctx context.Context, cfg Config) error {
	if ctx == nil {
		ctx = context.Background()
	}

	runtime, err := New(cfg)
	if err != nil {
		return err
	}

	if err := runtime.Start(ctx); err != nil {
		return err
	}

	waitErr := runtime.Wait(ctx)
	if ctx.Err() != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
		defer cancel()

		return runtime.Stop(stopCtx)
	}

	if waitErr != nil {
		_ = runtime.Stop(context.Background())

		return waitErr
	}

	return nil
}

// Start starts the Boyan standalone runtime.
func (runtime *Runtime) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	if runtime == nil || runtime.service == nil {
		return fmt.Errorf("Boyan runtime is unavailable")
	}

	return runtime.service.Start(ctx)
}

// Stop stops the Boyan standalone runtime.
func (runtime *Runtime) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	if runtime == nil || runtime.service == nil {
		return nil
	}

	return runtime.service.Stop(ctx)
}

// Wait blocks until the runtime stops or ctx is canceled.
func (runtime *Runtime) Wait(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	if runtime == nil || runtime.service == nil {
		return fmt.Errorf("Boyan runtime is unavailable")
	}

	if !runtime.service.startedState() {
		return fmt.Errorf("Boyan runtime is not started")
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-runtime.service.done:
		return runtime.service.terminalResult()
	}
}

// Ready reports whether the runtime has completed startup and is still serving.
func (runtime *Runtime) Ready() bool {
	return runtime != nil && runtime.service != nil && runtime.service.ready()
}

// Addr returns the configured or bound HTTP listener address.
func (runtime *Runtime) Addr() string {
	if runtime == nil || runtime.service == nil {
		return ""
	}

	return runtime.service.address()
}

// Handler returns the standalone HTTP handler.
func (runtime *Runtime) Handler() stdhttp.Handler {
	if runtime == nil || runtime.handler == nil {
		return stdhttp.NotFoundHandler()
	}

	return runtime.handler
}

// MCP returns the standalone MCP capability surface.
func (runtime *Runtime) MCP() *mcp.Service {
	if runtime == nil {
		return nil
	}

	return runtime.mcp
}

// IngestSequencedTurn applies one ordered turn ingest request.
func (runtime *Runtime) IngestSequencedTurn(
	ctx context.Context,
	request memory.SequencedTurn,
) (
	memory.IngestOutcome,
	error,
) {
	if runtime == nil || runtime.service == nil {
		return memory.IngestOutcome{}, fmt.Errorf("Boyan runtime is unavailable")
	}

	ctx, done, err := runtime.service.beginOperation(ctx)
	if err != nil {
		return memory.IngestOutcome{}, err
	}
	defer done()

	return runtime.service.sequenced.IngestSequencedTurn(ctx, request)
}

// ApplySequencedBoundary applies one ordered boundary ingest request.
func (runtime *Runtime) ApplySequencedBoundary(
	ctx context.Context,
	request memory.SequencedBoundary,
) (
	memory.IngestOutcome,
	error,
) {
	if runtime == nil || runtime.service == nil {
		return memory.IngestOutcome{}, fmt.Errorf("Boyan runtime is unavailable")
	}

	ctx, done, err := runtime.service.beginOperation(ctx)
	if err != nil {
		return memory.IngestOutcome{}, err
	}
	defer done()

	return runtime.service.sequenced.ApplySequencedBoundary(ctx, request)
}

// Search executes one bounded Boyan recall request.
func (runtime *Runtime) Search(ctx context.Context, request memory.RecallRequest) (memory.RecallResponse, error) {
	if runtime == nil || runtime.service == nil {
		return memory.RecallResponse{}, fmt.Errorf("Boyan runtime is unavailable")
	}

	ctx, done, err := runtime.service.beginOperation(ctx)
	if err != nil {
		return memory.RecallResponse{}, err
	}
	defer done()

	return runtime.service.recall.Search(ctx, request)
}

// Trace executes one bounded Boyan provenance request.
func (runtime *Runtime) Trace(ctx context.Context, request memory.TraceRequest) (memory.TraceResponse, error) {
	if runtime == nil || runtime.service == nil {
		return memory.TraceResponse{}, fmt.Errorf("Boyan runtime is unavailable")
	}

	ctx, done, err := runtime.service.beginOperation(ctx)
	if err != nil {
		return memory.TraceResponse{}, err
	}
	defer done()

	return runtime.service.trace.Trace(ctx, request)
}

// ForgetSource forgets one immutable source within one Boyan scope.
func (runtime *Runtime) ForgetSource(
	ctx context.Context,
	command memory.ForgetSourceCommand,
) (
	memory.ForgetOutcome,
	error,
) {
	if runtime == nil || runtime.service == nil {
		return memory.ForgetOutcome{}, fmt.Errorf("Boyan runtime is unavailable")
	}

	ctx, done, err := runtime.service.beginOperation(ctx)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}
	defer done()

	return runtime.service.forget.ForgetSource(ctx, command)
}

// ForgetScope forgets one exact Boyan scope.
func (runtime *Runtime) ForgetScope(
	ctx context.Context,
	command memory.ForgetScopeCommand,
) (
	memory.ForgetOutcome,
	error,
) {
	if runtime == nil || runtime.service == nil {
		return memory.ForgetOutcome{}, fmt.Errorf("Boyan runtime is unavailable")
	}

	ctx, done, err := runtime.service.beginOperation(ctx)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}
	defer done()

	return runtime.service.forget.ForgetScope(ctx, command)
}
