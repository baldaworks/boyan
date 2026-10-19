package boyan

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/baldaworks/boyan/internal/embedding"
	semanticcore "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/internal/standaloneruntime"
	"github.com/baldaworks/boyan/mcp"
	memory "github.com/baldaworks/boyan/pkg/memory"
	canonicalsqlite "github.com/baldaworks/boyan/store/sqlite"
)

type hostRuntime interface {
	hostLifecycle
	hostTransport
	hostCapabilities
}

type hostLifecycle interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Wait(ctx context.Context) error
}

type hostTransport interface {
	Ready() bool
	Addr() string
	Handler() http.Handler
	MCP() *mcp.Service
}

type hostCapabilities interface {
	IngestSequencedTurn(ctx context.Context, request memory.SequencedTurn) (memory.IngestOutcome, error)
	ApplySequencedBoundary(ctx context.Context, request memory.SequencedBoundary) (memory.IngestOutcome, error)
	Search(ctx context.Context, request memory.RecallRequest) (memory.RecallResponse, error)
	Trace(ctx context.Context, request memory.TraceRequest) (memory.TraceResponse, error)
	ForgetSource(ctx context.Context, command memory.ForgetSourceCommand) (memory.ForgetOutcome, error)
	ForgetScope(ctx context.Context, command memory.ForgetScopeCommand) (memory.ForgetOutcome, error)
}

// Host is the public Boyan embedding surface.
type Host struct {
	config  Config
	runtime hostRuntime
}

// New composes and preflights a Boyan host.
func New(ctx context.Context, config Config) (*Host, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}

	normalized, err := config.normalized()
	if err != nil {
		return nil, err
	}

	credential, err := normalized.Embeddings.Credential()
	if err != nil {
		return nil, err
	}

	runtimeConfig := standaloneRuntimeConfig(normalized)

	runtimeConfig.Canonical, err = normalized.resolveCanonical(ctxOrBackground(ctx))
	if err != nil {
		return nil, err
	}

	if normalized.Embeddings.Enabled {
		settings := normalized.Embeddings

		provider, providerErr := embedding.NewOpenAIProvider(embedding.OpenAIOptions{
			Endpoint: settings.Endpoint, Model: settings.Model, Dimensions: settings.Dimensions, APIKey: credential,
		})
		if providerErr != nil {
			return nil, ErrInvalidEmbeddingsConfig
		}

		runtimeConfig.Embeddings = standaloneruntime.EmbeddingsConfig{
			Provider: provider,
			ModelSpace: semanticcore.ModelSpace{
				Model: settings.Model, Revision: settings.Revision, Dimensions: settings.Dimensions,
				QueryPrefix: settings.QueryPrefix, PassagePrefix: settings.PassagePrefix,
			},
		}
	}

	runtime, err := standaloneruntime.NewContext(ctxOrBackground(ctx), runtimeConfig)
	if err != nil {
		return nil, err
	}

	return newHost(normalized, runtime), nil
}

// NewHost is an explicit constructor alias for callers composing one local host.
func NewHost(ctx context.Context, config Config) (*Host, error) {
	return New(ctx, config)
}

func newHost(config Config, runtime hostRuntime) *Host {
	return &Host{config: config, runtime: runtime}
}

// Start starts the Boyan host runtime.
func (host *Host) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	if host == nil || host.runtime == nil {
		return fmt.Errorf("Boyan host runtime is unavailable")
	}

	return host.runtime.Start(ctx)
}

// Run starts the host and blocks until ctx is canceled or the runtime fails.
func (host *Host) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	if err := host.Start(ctx); err != nil {
		return err
	}

	waitErr := host.Wait(ctx)
	if ctx.Err() != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), host.config.HTTP.ShutdownTimeout)
		defer cancel()

		return host.Stop(shutdownCtx)
	}

	if waitErr != nil {
		_ = host.Stop(context.Background())

		if errors.Is(waitErr, context.Canceled) || errors.Is(waitErr, context.DeadlineExceeded) {
			return nil
		}

		return waitErr
	}

	return nil
}

// Wait blocks until the host runtime stops or ctx is canceled.
func (host *Host) Wait(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	if host == nil || host.runtime == nil {
		return fmt.Errorf("Boyan host runtime is unavailable")
	}

	return host.runtime.Wait(ctx)
}

// Stop stops the Boyan host runtime.
func (host *Host) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	if host == nil || host.runtime == nil {
		return nil
	}

	return host.runtime.Stop(ctx)
}

// Close shuts down the host with its configured timeout.
func (host *Host) Close() error {
	if host == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), host.config.HTTP.ShutdownTimeout)
	defer cancel()

	return host.Stop(ctx)
}

// Ready reports whether the host is ready to accept work.
func (host *Host) Ready() bool {
	return host != nil && host.runtime != nil && host.runtime.Ready()
}

// Addr returns the bound HTTP address, or the configured address before Start.
func (host *Host) Addr() string {
	if host == nil {
		return ""
	}

	if host.runtime != nil {
		return host.runtime.Addr()
	}

	return host.config.HTTP.Listen
}

// Handler returns the loopback HTTP handler for in-process integration and embedding.
func (host *Host) Handler() http.Handler {
	if host != nil && host.runtime != nil && host.runtime.Handler() != nil {
		return host.runtime.Handler()
	}

	return http.NotFoundHandler()
}

// MCP returns the public Boyan MCP capability surface.
func (host *Host) MCP() *mcp.Service {
	if host == nil || host.runtime == nil {
		return nil
	}

	return host.runtime.MCP()
}

// IngestSequencedTurn applies one ordered turn ingest request.
func (host *Host) IngestSequencedTurn(ctx context.Context, request memory.SequencedTurn) (memory.IngestOutcome, error) {
	if host == nil || host.runtime == nil {
		return memory.IngestOutcome{}, fmt.Errorf("Boyan host runtime is unavailable")
	}

	return host.runtime.IngestSequencedTurn(ctx, request)
}

// ApplySequencedBoundary applies one ordered boundary ingest request.
func (host *Host) ApplySequencedBoundary(
	ctx context.Context,
	request memory.SequencedBoundary,
) (
	memory.IngestOutcome,
	error,
) {
	if host == nil || host.runtime == nil {
		return memory.IngestOutcome{}, fmt.Errorf("Boyan host runtime is unavailable")
	}

	return host.runtime.ApplySequencedBoundary(ctx, request)
}

// Search executes one bounded Boyan recall request.
func (host *Host) Search(ctx context.Context, request memory.RecallRequest) (memory.RecallResponse, error) {
	if host == nil || host.runtime == nil {
		return memory.RecallResponse{}, fmt.Errorf("Boyan host runtime is unavailable")
	}

	return host.runtime.Search(ctx, request)
}

// Trace executes one bounded Boyan provenance request.
func (host *Host) Trace(ctx context.Context, request memory.TraceRequest) (memory.TraceResponse, error) {
	if host == nil || host.runtime == nil {
		return memory.TraceResponse{}, fmt.Errorf("Boyan host runtime is unavailable")
	}

	return host.runtime.Trace(ctx, request)
}

// ForgetSource forgets one immutable source within one Boyan scope.
// Nil error includes joined logical search retirement and lexical sync, without
// waiting for semantic inference or promising physical erasure. On a later
// cleanup error the original durable outcome may be returned; retry the same operation.
func (host *Host) ForgetSource(ctx context.Context, command memory.ForgetSourceCommand) (memory.ForgetOutcome, error) {
	if host == nil || host.runtime == nil {
		return memory.ForgetOutcome{}, fmt.Errorf("Boyan host runtime is unavailable")
	}

	return host.runtime.ForgetSource(ctx, command)
}

// ForgetScope forgets one exact Boyan scope.
// Nil error includes joined logical search retirement and lexical sync. Error
// or cancellation may leave a durable outcome and partial retirement; retry the
// same operation to preserve its original boundary and outcome.
func (host *Host) ForgetScope(ctx context.Context, command memory.ForgetScopeCommand) (memory.ForgetOutcome, error) {
	if host == nil || host.runtime == nil {
		return memory.ForgetOutcome{}, fmt.Errorf("Boyan host runtime is unavailable")
	}

	return host.runtime.ForgetScope(ctx, command)
}

func standaloneRuntimeConfig(config Config) standaloneruntime.Config {
	return standaloneruntime.Config{
		StateDir: config.StateDir,
		Canonical: standaloneruntime.CanonicalConfig{
			Backend: config.Canonical.Backend,
			SQLite: canonicalsqlite.Config{
				Path:        config.Canonical.SQLite.Path,
				BusyTimeout: config.Canonical.SQLite.BusyTimeout,
			},
		},
		Log: standaloneruntime.LogConfig{
			Level: config.Log.Level,
		},
		HTTP: standaloneruntime.HTTPConfig{
			Listen:          config.HTTP.Listen,
			RequestTimeout:  config.HTTP.RequestTimeout,
			ShutdownTimeout: config.HTTP.ShutdownTimeout,
			MaxBodyBytes:    config.HTTP.MaxBodyBytes,
		},
		Derivation: standaloneruntime.DerivationConfig{
			Provider:       config.Derivation.Provider,
			WorkingDir:     config.Derivation.WorkingDir,
			Timeout:        config.Derivation.Timeout,
			MaxOutputBytes: config.Derivation.MaxOutputBytes,
			Runtime:        config.Derivation.Runtime,
		},
		Maintenance: standaloneruntime.MaintenanceConfig{
			ValueLogGCInterval: config.Maintenance.ValueLogGCInterval,
			DiscardRatio:       config.Maintenance.DiscardRatio,
		},
	}
}

func ctxOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}

	return ctx
}
