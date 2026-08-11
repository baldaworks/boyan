package derivation

import (
	"fmt"
	"strings"

	adkagent "google.golang.org/adk/v2/agent"
)

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

func defaultRunnerBuilder(agent adkagent.Agent, stateStore runStateStore, appName string) (eventRunner, error) {
	return newRunEventRunner(agent, stateStore, appName)
}
