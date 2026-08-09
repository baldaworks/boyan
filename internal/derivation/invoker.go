package derivation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"strings"
	"sync"
	"time"

	"github.com/baldaworks/boyan"
	portableapp "github.com/baldaworks/boyan/app"
	"github.com/normahq/runtime/v2/agentfactory"
	"github.com/normahq/runtime/v2/structuredagent"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

type validatingFactory interface {
	ValidateAgent(agentID string) error
}

type runtimeFactory interface {
	validatingFactory
	Build(ctx context.Context, req agentfactory.BuildRequest) (adkagent.Agent, error)
}

type eventRunner interface {
	Run(ctx context.Context, userID, sessionID string, msg *genai.Content, cfg adkagent.RunConfig, opts ...runner.RunOption) iter.Seq2[*adksession.Event, error]
}

type builtRuntime struct {
	agent      adkagent.Agent
	sessionSvc adksession.Service
	appName    string
}

type runnerBuilder func(agent adkagent.Agent, sessionSvc adksession.Service, appName string) (eventRunner, error)

// Invoker is the Boyan-owned StructuredInvoker backed by an isolated runtime/v2
// provider runtime. Its session service is in-memory and never shared with
// user-facing chat sessions.
type Invoker struct {
	mu      sync.Mutex
	factory runtimeFactory
	runner  runnerBuilder

	providerID string
	workingDir string
	maxBytes   int
	timeout    time.Duration

	runtime *builtRuntime
	closed  bool
}

var _ portableapp.StructuredInvoker = (*Invoker)(nil)

// NewInvoker constructs an isolated derivation invoker from validated config.
func NewInvoker(cfg Config) (*Invoker, error) {
	return newInvokerWithFactory(cfg, newAgentFactory(cfg.Runtime), defaultRunnerBuilder)
}

func newInvokerWithFactory(cfg Config, factory runtimeFactory, makeRunner runnerBuilder) (*Invoker, error) {
	if err := cfg.validateWithFactory(factory); err != nil {
		return nil, err
	}
	if makeRunner == nil {
		return nil, fmt.Errorf("boyan derivation runner builder is required")
	}
	return &Invoker{
		factory:    factory,
		runner:     makeRunner,
		providerID: strings.TrimSpace(cfg.ProviderID),
		workingDir: strings.TrimSpace(cfg.WorkingDir),
		maxBytes:   cfg.maxOutputBytes(),
		timeout:    cfg.timeout(),
	}, nil
}

func defaultRunnerBuilder(agent adkagent.Agent, sessionSvc adksession.Service, appName string) (eventRunner, error) {
	return runner.New(runner.Config{
		AppName:        appName,
		Agent:          agent,
		SessionService: sessionSvc,
	})
}

// Invoke runs one bounded structured derivation operation.
func (i *Invoker) Invoke(ctx context.Context, request portableapp.StructuredInvocation) ([]byte, error) {
	if ctx == nil {
		return nil, boyan.PermanentError(boyan.CodeInvalidDerived, "Boyan derivation context is required", nil)
	}
	invokeCtx, cancel := context.WithTimeout(ctx, i.timeout)
	defer cancel()

	i.mu.Lock()
	if i.closed {
		i.mu.Unlock()
		return nil, boyan.PermanentError(boyan.CodeModelFailure, "Boyan derivation runtime is closed", nil)
	}
	if i.runtime == nil {
		runtime, err := i.buildRuntime(invokeCtx)
		if err != nil {
			i.mu.Unlock()
			return nil, err
		}
		i.runtime = runtime
	}
	runtime := i.runtime
	i.mu.Unlock()

	if runtime == nil || runtime.agent == nil {
		return nil, boyan.PermanentError(boyan.CodeModelFailure, "Boyan derivation agent is unavailable", nil)
	}
	if len(request.InputJSON) > boyan.MaxDerivedTurnTextBytes {
		return nil, boyan.PermanentError(boyan.CodeLimitExceeded, "Boyan derivation input exceeds the limit", nil)
	}
	if strings.TrimSpace(request.OutputSchema) == "" {
		return nil, boyan.PermanentError(boyan.CodeInvalidDerived, "Boyan derivation output schema is required", nil)
	}

	wrapped, err := structuredagent.NewAgent(
		runtime.agent,
		structuredagent.WithSystemInstruction(request.Instruction),
		structuredagent.WithOutputSchema(request.OutputSchema),
		structuredagent.WithMaxAccumulatedOutputBytes(i.maxBytes),
		structuredagent.WithOutputValidationRetries(1),
	)
	if err != nil {
		return nil, boyan.PermanentError(boyan.CodeModelFailure, "build Boyan structured derivation wrapper", err)
	}
	run, err := i.runner(wrapped, runtime.sessionSvc, runtime.appName)
	if err != nil {
		return nil, boyan.PermanentError(boyan.CodeModelFailure, "build Boyan derivation runner", err)
	}

	sessionID := processorSessionID(request.OperationID, request.Stage)
	_ = runtime.sessionSvc.Delete(invokeCtx, &adksession.DeleteRequest{
		AppName:   runtime.appName,
		UserID:    defaultDerivationUser,
		SessionID: sessionID,
	})
	if _, err := runtime.sessionSvc.Create(invokeCtx, &adksession.CreateRequest{
		AppName:   runtime.appName,
		UserID:    defaultDerivationUser,
		SessionID: sessionID,
	}); err != nil {
		if errors.Is(invokeCtx.Err(), context.DeadlineExceeded) {
			return nil, boyan.RetryableError(boyan.CodeTimeout, "create Boyan derivation session timed out", nil)
		}
		return nil, boyan.RetryableError(boyan.CodeModelFailure, "create Boyan derivation session", nil)
	}
	defer func() {
		_ = runtime.sessionSvc.Delete(context.Background(), &adksession.DeleteRequest{
			AppName:   runtime.appName,
			UserID:    defaultDerivationUser,
			SessionID: sessionID,
		})
	}()

	envelope, err := json.Marshal(map[string]string{"input": string(request.InputJSON)})
	if err != nil {
		return nil, boyan.PermanentError(boyan.CodeInvalidDerived, "encode Boyan derivation input", err)
	}

	var output string
	for event, runErr := range run.Run(
		invokeCtx,
		defaultDerivationUser,
		sessionID,
		genai.NewContentFromText(string(envelope), genai.RoleUser),
		adkagent.RunConfig{},
	) {
		if runErr != nil {
			if errors.Is(invokeCtx.Err(), context.DeadlineExceeded) {
				return nil, boyan.RetryableError(boyan.CodeTimeout, "run Boyan derivation timed out", nil)
			}
			return nil, boyan.RetryableError(boyan.CodeModelFailure, "run Boyan derivation", nil)
		}
		if event == nil || event.Content == nil {
			continue
		}
		var text strings.Builder
		for _, part := range event.Content.Parts {
			if part != nil && !part.Thought {
				text.WriteString(part.Text)
			}
		}
		if text.Len() > 0 {
			output = text.String()
		}
	}
	if strings.TrimSpace(output) == "" {
		if errors.Is(invokeCtx.Err(), context.DeadlineExceeded) {
			return nil, boyan.RetryableError(boyan.CodeTimeout, "Boyan derivation timed out", nil)
		}
		return nil, boyan.RetryableError(boyan.CodeModelFailure, "Boyan derivation produced no output", nil)
	}
	if len(output) > i.maxBytes {
		return nil, boyan.PermanentError(boyan.CodeLimitExceeded, "Boyan derivation output exceeds the limit", nil)
	}
	return []byte(output), nil
}

func (i *Invoker) buildRuntime(ctx context.Context) (*builtRuntime, error) {
	req, err := dedicatedRuntimeBuildRequest(i.providerID, i.workingDir, portableapp.DefaultDerivationInstruction)
	if err != nil {
		return nil, err
	}
	agent, err := i.factory.Build(ctx, req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, boyan.RetryableError(boyan.CodeTimeout, "build Boyan derivation runtime timed out", nil)
		}
		return nil, boyan.RetryableError(boyan.CodeModelFailure, "build Boyan derivation runtime", nil)
	}
	return &builtRuntime{
		agent:      agent,
		sessionSvc: adksession.InMemoryService(),
		appName:    defaultRuntimeAppName,
	}, nil
}

// Close releases resources owned by the isolated derivation runtime.
func (i *Invoker) Close(context.Context) error {
	if i == nil {
		return nil
	}
	i.mu.Lock()
	if i.closed {
		i.mu.Unlock()
		return nil
	}
	i.closed = true
	runtime := i.runtime
	i.runtime = nil
	i.mu.Unlock()

	if runtime == nil || runtime.agent == nil {
		return nil
	}
	if closer, ok := runtime.agent.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func dedicatedRuntimeBuildRequest(providerID, workingDir, instruction string) (agentfactory.BuildRequest, error) {
	trimmedProvider := strings.TrimSpace(providerID)
	if trimmedProvider == "" {
		return agentfactory.BuildRequest{}, fmt.Errorf("Boyan derivation provider is required")
	}
	trimmedDir := strings.TrimSpace(workingDir)
	if trimmedDir == "" {
		return agentfactory.BuildRequest{}, fmt.Errorf("Boyan derivation workspace is required")
	}
	return agentfactory.BuildRequest{
		AgentID:          trimmedProvider,
		Name:             trimmedProvider + "-boyan-derivation",
		Description:      "Dedicated Boyan structured derivation runtime",
		WorkingDirectory: trimmedDir,
		Instruction:      strings.TrimSpace(instruction),
		MCPServerIDs:     []string{},
	}, nil
}

func processorSessionID(operationID, stage string) string {
	hash := sha256.Sum256([]byte(operationID + "\x00" + stage))
	return "boyan-derivation-" + hex.EncodeToString(hash[:])
}
