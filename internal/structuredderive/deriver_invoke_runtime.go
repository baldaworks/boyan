package structuredderive

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (d *Deriver) invoke(ctx context.Context, request StructuredInvocation, output any) error {
	if ctx == nil {
		return memory.PermanentError(memory.CodeInvalidDerived, "derivation context is required", nil)
	}
	if ctx.Err() != nil {
		return deriverContextError(ctx)
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
	if ctx.Err() != nil {
		return deriverContextError(ctx)
	}

	if len(raw) == 0 || len(raw) > d.maxOutputBytes {
		return memory.PermanentError(memory.CodeLimitExceeded, "derivation output exceeds the configured limit", nil)
	}

	return decodeDerivedOutput(raw, request.Stage, output)
}

func deriverContextError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return memory.RetryableError(memory.CodeTimeout, "structured derivation timed out", nil)
	}

	return memory.RetryableError(memory.CodeModelFailure, "structured derivation was canceled", nil)
}

func decodeDerivedOutput(raw []byte, stage string, output any) error {
	validated, ok := validatedOutput(raw, stage)
	if !ok {
		return memory.PermanentError(memory.CodeInvalidDerived, "structured derivation output is invalid", nil)
	}

	if err := json.Unmarshal(validated, output); err != nil {
		return memory.PermanentError(memory.CodeInvalidDerived, "structured derivation output is invalid", nil)
	}

	return nil
}

func (d *Deriver) invokeLegacyStage(ctx context.Context, input any, stage memory.OperationStage,
	exportID, instruction string, derivation memory.DerivationRef, output any) error {
	encoded, err := json.Marshal(input)
	if err != nil {
		return memory.PermanentError(memory.CodeInvalidDerived, "encode derivation request", err)
	}

	identity, err := operationID(stage, exportID, derivation)
	if err != nil {
		return err
	}

	return d.invoke(ctx, StructuredInvocation{OperationID: identity, Stage: string(stage),
		Instruction: instruction, InputJSON: encoded, OutputSchema: outputSchema(string(stage))}, output)
}
