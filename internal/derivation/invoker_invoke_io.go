package derivation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/baldaworks/boyan/internal/structuredderive"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"google.golang.org/genai"
)

func validateStructuredInvocationRequest(request structuredderive.StructuredInvocation) error {
	if len(request.InputJSON) > memory.MaxDerivedTurnTextBytes {
		return memory.PermanentError(memory.CodeLimitExceeded, "Boyan derivation input exceeds the limit", nil)
	}

	if strings.TrimSpace(request.OutputSchema) == "" {
		return memory.PermanentError(memory.CodeInvalidDerived, "Boyan derivation output schema is required", nil)
	}

	return nil
}

func marshalStructuredInvocationInput(request structuredderive.StructuredInvocation) (*genai.Content, error) {
	envelope, err := json.Marshal(map[string]string{"input": string(request.InputJSON)})
	if err != nil {
		return nil, memory.PermanentError(memory.CodeInvalidDerived, "encode Boyan derivation input", err)
	}

	return genai.NewContentFromText(string(envelope), genai.RoleUser), nil
}

func validateStructuredInvocationOutput(ctx context.Context, output string, maxBytes int) ([]byte, error) {
	if strings.TrimSpace(output) == "" {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, memory.RetryableError(memory.CodeTimeout, "Boyan derivation timed out", nil)
		}

		return nil, memory.RetryableError(memory.CodeModelFailure, "Boyan derivation produced no output", nil)
	}

	if len(output) > maxBytes {
		return nil, memory.PermanentError(memory.CodeLimitExceeded, "Boyan derivation output exceeds the limit", nil)
	}

	return []byte(output), nil
}
