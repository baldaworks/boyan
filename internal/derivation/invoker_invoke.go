package derivation

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"strings"

	portableapp "github.com/baldaworks/boyan/internal/apps/boyan"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/normahq/runtime/v2/structuredagent"
	adkagent "google.golang.org/adk/v2/agent"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// Invoke runs one bounded structured derivation operation.
func (i *Invoker) Invoke(ctx context.Context, request portableapp.StructuredInvocation) ([]byte, error) {
	if ctx == nil {
		return nil, memory.PermanentError(memory.CodeInvalidDerived, "Boyan derivation context is required", nil)
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
	if len(request.InputJSON) > memory.MaxDerivedTurnTextBytes {
		return nil, memory.PermanentError(memory.CodeLimitExceeded, "Boyan derivation input exceeds the limit", nil)
	}
	if strings.TrimSpace(request.OutputSchema) == "" {
		return nil, memory.PermanentError(memory.CodeInvalidDerived, "Boyan derivation output schema is required", nil)
	}

	run, err := i.buildStructuredRunner(runtime, request)
	if err != nil {
		return nil, err
	}
	sessionID, err := i.prepareSession(invokeCtx, runtime, request)
	if err != nil {
		return nil, err
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
		return nil, memory.PermanentError(memory.CodeInvalidDerived, "encode Boyan derivation input", err)
	}
	output, err := collectRunnerOutput(
		invokeCtx,
		run.Run(
			invokeCtx,
			defaultDerivationUser,
			sessionID,
			genai.NewContentFromText(string(envelope), genai.RoleUser),
			adkagent.RunConfig{},
		),
	)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(output) == "" {
		if errors.Is(invokeCtx.Err(), context.DeadlineExceeded) {
			return nil, memory.RetryableError(memory.CodeTimeout, "Boyan derivation timed out", nil)
		}
		return nil, memory.RetryableError(memory.CodeModelFailure, "Boyan derivation produced no output", nil)
	}
	if len(output) > i.maxBytes {
		return nil, memory.PermanentError(memory.CodeLimitExceeded, "Boyan derivation output exceeds the limit", nil)
	}
	return []byte(output), nil
}

func (i *Invoker) buildStructuredRunner(runtime *builtRuntime, request portableapp.StructuredInvocation) (eventRunner, error) {
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
	run, err := i.runner(wrapped, runtime.sessionSvc, runtime.appName)
	if err != nil {
		return nil, memory.PermanentError(memory.CodeModelFailure, "build Boyan derivation runner", err)
	}
	return run, nil
}

func (i *Invoker) prepareSession(ctx context.Context, runtime *builtRuntime, request portableapp.StructuredInvocation) (string, error) {
	sessionID := processorSessionID(request.OperationID, request.Stage)
	_ = runtime.sessionSvc.Delete(ctx, &adksession.DeleteRequest{
		AppName:   runtime.appName,
		UserID:    defaultDerivationUser,
		SessionID: sessionID,
	})
	if _, err := runtime.sessionSvc.Create(ctx, &adksession.CreateRequest{
		AppName:   runtime.appName,
		UserID:    defaultDerivationUser,
		SessionID: sessionID,
	}); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", memory.RetryableError(memory.CodeTimeout, "create Boyan derivation session timed out", nil)
		}
		return "", memory.RetryableError(memory.CodeModelFailure, "create Boyan derivation session", nil)
	}
	return sessionID, nil
}

func collectRunnerOutput(ctx context.Context, events iter.Seq2[*adksession.Event, error]) (string, error) {
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
