package derivation

import (
	"context"
	"fmt"
	"iter"
	"strings"
	"sync"
	"time"

	portableapp "github.com/baldaworks/boyan/internal/apps/boyan"
	"github.com/normahq/runtime/v2/agentfactory"
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

// Invoker is the Boyan-owned StructuredInvoker backed by an isolated provider
// runtime. Its session service is in-memory and never shared with user-facing
// chat sessions.
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
