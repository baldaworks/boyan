package structuredderive

import (
	"context"
	"encoding/json"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (d *Deriver) invoke(ctx context.Context, request StructuredInvocation, output any) error {
	if ctx == nil {
		return memory.PermanentError(memory.CodeInvalidDerived, "derivation context is required", nil)
	}

	if len(request.InputJSON) > d.maxInputBytes {
		return memory.PermanentError(memory.CodeLimitExceeded, "derivation input exceeds the configured limit", nil)
	}

	if strings.TrimSpace(request.OperationID) == "" ||
		strings.TrimSpace(request.Stage) == "" {
		return memory.PermanentError(memory.CodeInvalidDerived, "derivation operation identity is required", nil)
	}

	if output == nil {
		return memory.PermanentError(memory.CodeInvalidDerived, "derivation output target is required", nil)
	}

	raw, err := d.invoker.Invoke(ctx, request)
	if err != nil {
		return classifyDeriverError(err)
	}

	if len(raw) == 0 || len(raw) > d.maxOutputBytes {
		return memory.PermanentError(memory.CodeLimitExceeded, "derivation output exceeds the configured limit", nil)
	}

	if err := json.Unmarshal(raw, output); err != nil {
		return memory.PermanentError(memory.CodeInvalidDerived, "structured derivation output is invalid", nil)
	}

	return nil
}
