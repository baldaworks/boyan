package structuredderive

import (
	"context"
	"encoding/json"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// ExtractAtoms implements the legacy atom model port while hosts migrate to
// canonical semantic extraction. It remains here as a pure application
// adapter; the host-specific provider is only a StructuredInvoker.
func (d *Deriver) ExtractAtoms(ctx context.Context, request memory.AtomExtractionRequest) ([]memory.AtomCandidate, error) {
	if err := d.available(); err != nil {
		return nil, err
	}
	request.Derivation = normalizeDerivation(request.Derivation)
	input, err := json.Marshal(request)
	if err != nil {
		return nil, memory.PermanentError(memory.CodeInvalidDerived, "encode atom derivation request", err)
	}
	operationID, err := operationID(memory.OperationStageAtoms, request.Turn.ExportID, request.Derivation)
	if err != nil {
		return nil, err
	}
	var output atomsOutput
	if err := d.invoke(ctx, StructuredInvocation{
		OperationID:  operationID,
		Stage:        string(memory.OperationStageAtoms),
		Instruction:  DefaultDerivationInstruction + " Extract only grounded atom candidates from this completed turn.",
		InputJSON:    input,
		OutputSchema: atomsOutputSchema,
	}, &output); err != nil {
		return nil, err
	}
	return output.Output, nil
}
