package derivation

import (
	"context"
	"iter"
	"sync"
	"time"

	"github.com/baldaworks/boyan/internal/structuredderive"
	"github.com/normahq/runtime/v2/agentfactory"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/runner"
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
	Run(
		ctx context.Context,
		userID, stateID string,
		msg *genai.Content,
		cfg adkagent.RunConfig,
		opts ...runner.RunOption,
	) iter.Seq2[*runEvent, error]
}

type builtRuntime struct {
	agent      adkagent.Agent
	stateStore runStateStore
	appName    string
}

type runnerBuilder func(agent adkagent.Agent, stateStore runStateStore, appName string) (eventRunner, error)

// Invoker is the Boyan-owned StructuredInvoker backed by an isolated provider
// runtime. It keeps ephemeral ADK run state private to derivation work and
// never shares that state with user-facing conversations.
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

var _ structuredderive.StructuredInvoker = (*Invoker)(nil)
