package structuredderive

import (
	"context"
	"encoding/json"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// ExtractCanonicalSemantics implements the canonical semantic model port.
func (d *Deriver) ExtractCanonicalSemantics(
	ctx context.Context,
	request memory.CanonicalExtractionRequest,
) (
	[]memory.SemanticCandidate,
	error,
) {
	if err := d.available(); err != nil {
		return nil, err
	}

	request.Derivation = normalizeDerivation(request.Derivation)
	if err := request.Validate(); err != nil {
		return nil, err
	}

	input, err := json.Marshal(request)
	if err != nil {
		return nil, memory.PermanentError(memory.CodeInvalidDerived, "encode canonical semantic derivation request", err)
	}

	operationID, err := memory.CanonicalSemanticOperationID(request.Turn.ExportID, request.Derivation)
	if err != nil {
		return nil, err
	}

	var output canonicalSemanticsOutput
	if err := d.invoke(ctx, StructuredInvocation{
		OperationID: operationID,
		Stage:       "canonical_semantics",
		Instruction: DefaultDerivationInstruction + (" Extract only grounded canonical semantic candidates with me" +
			"ssage-level evidence."),
		InputJSON:    input,
		OutputSchema: outputSchema("canonical_semantics"),
	}, &output); err != nil {
		return nil, err
	}

	return output.Output, nil
}
