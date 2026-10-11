package derivation

import (
	"context"
	"fmt"

	portableapp "github.com/baldaworks/boyan/internal/portableapp"
)

// NewRuntime composes a Boyan derivation invoker through the portable app
// runtime. Callers must leave Extractor and StructuredInvoker unset so this
// helper remains the single derivation owner.
func NewRuntime(cfg Config, runtimeCfg portableapp.RuntimeConfig) (*portableapp.Runtime, error) {
	return newRuntimeWithFactory(cfg, runtimeCfg, newAgentFactory(cfg.Runtime), defaultRunnerBuilder)
}

func newRuntimeWithFactory(
	cfg Config,
	runtimeCfg portableapp.RuntimeConfig,
	factory runtimeFactory,
	makeRunner runnerBuilder,
) (*portableapp.Runtime, error) {
	if runtimeCfg.Extractor != nil {
		return nil, fmt.Errorf("boyan derivation runtime requires Extractor to be unset")
	}

	if runtimeCfg.StructuredInvoker != nil {
		return nil, fmt.Errorf("boyan derivation runtime requires StructuredInvoker to be unset")
	}

	invoker, err := newInvokerWithFactory(cfg, factory, makeRunner)
	if err != nil {
		return nil, err
	}

	runtimeCfg.StructuredInvoker = invoker

	runtime, err := portableapp.NewRuntime(runtimeCfg)
	if err != nil {
		_ = invoker.Close(context.Background())

		return nil, err
	}

	return runtime, nil
}
