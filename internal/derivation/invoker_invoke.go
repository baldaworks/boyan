package derivation

import (
	"context"

	"github.com/baldaworks/boyan/internal/structuredderive"
	memory "github.com/baldaworks/boyan/pkg/memory"
	adkagent "google.golang.org/adk/v2/agent"
)

// Invoke runs one bounded structured derivation operation.
func (i *Invoker) Invoke(ctx context.Context, request structuredderive.StructuredInvocation) ([]byte, error) {
	if ctx == nil {
		return nil, memory.PermanentError(memory.CodeInvalidDerived, "Boyan derivation context is required", nil)
	}
	if err := validateStructuredInvocationRequest(request); err != nil {
		return nil, err
	}

	invokeCtx, cancel := context.WithTimeout(ctx, i.timeout)
	defer cancel()

	runtime, err := i.ensureRuntime(invokeCtx)
	if err != nil {
		return nil, err
	}

	if runtime == nil || runtime.agent == nil {
		return nil, memory.PermanentError(memory.CodeModelFailure, "Boyan derivation agent is unavailable", nil)
	}

	run, err := i.buildStructuredRunner(runtime, request)
	if err != nil {
		return nil, err
	}

	runStateID, err := i.prepareRunState(invokeCtx, runtime)
	if err != nil {
		return nil, err
	}

	defer func() {
		deleteRunState(context.Background(), runtime.stateStore, runtime.appName, runStateID)
	}()

	input, err := marshalStructuredInvocationInput(request)
	if err != nil {
		return nil, err
	}

	output, err := collectRunnerOutput(
		invokeCtx,
		run.Run(
			invokeCtx,
			defaultDerivationUser,
			runStateID,
			input,
			adkagent.RunConfig{},
		),
	)
	if err != nil {
		return nil, err
	}

	return validateStructuredInvocationOutput(invokeCtx, output, i.maxBytes)
}
