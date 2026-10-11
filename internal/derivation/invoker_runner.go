package derivation

import (
	"context"
	"errors"
	"iter"
	"strings"

	"github.com/baldaworks/boyan/internal/structuredderive"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/normahq/runtime/v2/structuredagent"
)

func (i *Invoker) buildStructuredRunner(
	runtime *builtRuntime,
	request structuredderive.StructuredInvocation,
) (
	eventRunner,
	error,
) {
	wrapped, err := structuredagent.NewAgent(
		runtime.agent,
		structuredagent.WithSystemInstruction(request.Instruction),
		structuredagent.WithOutputSchema(request.OutputSchema),
		structuredagent.WithMaxAccumulatedOutputBytes(i.maxBytes),
		structuredagent.WithOutputValidationRetries(1),
	)
	if err != nil {
		return nil, memory.PermanentError(memory.CodeModelFailure, "build Boyan structured derivation wrapper", err)
	}

	run, err := i.runner(wrapped, runtime.stateStore, runtime.appName)
	if err != nil {
		return nil, memory.PermanentError(memory.CodeModelFailure, "build Boyan derivation runner", err)
	}

	return run, nil
}

func collectRunnerOutput(ctx context.Context, events iter.Seq2[*runEvent, error]) (string, error) {
	var output string

	for event, runErr := range events {
		if runErr != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return "", memory.RetryableError(memory.CodeTimeout, "run Boyan derivation timed out", nil)
			}

			return "", memory.RetryableError(memory.CodeModelFailure, "run Boyan derivation", nil)
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

	return output, nil
}
