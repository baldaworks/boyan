package structuredderive

import (
	"context"
	"encoding/json"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// SynthesizeScenarios implements boundary scenario synthesis.
func (d *Deriver) SynthesizeScenarios(ctx context.Context, request memory.ScenarioSynthesisRequest) ([]memory.ScenarioCandidate, error) {
	if err := d.available(); err != nil {
		return nil, err
	}
	request.Derivation = normalizeDerivation(request.Derivation)
	input, err := json.Marshal(request)
	if err != nil {
		return nil, memory.PermanentError(memory.CodeInvalidDerived, "encode scenario derivation request", err)
	}
	operationID, err := operationID(memory.OperationStageScenarios, request.Boundary.ExportID, request.Derivation)
	if err != nil {
		return nil, err
	}
	var output scenariosOutput
	if err := d.invoke(ctx, StructuredInvocation{
		OperationID:  operationID,
		Stage:        string(memory.OperationStageScenarios),
		Instruction:  DefaultDerivationInstruction + " Synthesize same-scope topic scenarios from the active view.",
		InputJSON:    input,
		OutputSchema: scenariosOutputSchema,
	}, &output); err != nil {
		return nil, err
	}
	return output.Output, nil
}

// SynthesizeProfile implements exact-scope profile synthesis.
func (d *Deriver) SynthesizeProfile(ctx context.Context, request memory.ProfileSynthesisRequest) (*memory.ProfileCandidate, error) {
	if err := d.available(); err != nil {
		return nil, err
	}
	request.Derivation = normalizeDerivation(request.Derivation)
	input, err := json.Marshal(request)
	if err != nil {
		return nil, memory.PermanentError(memory.CodeInvalidDerived, "encode profile derivation request", err)
	}
	operationID, err := operationID(memory.OperationStageProfile, request.Boundary.ExportID, request.Derivation)
	if err != nil {
		return nil, err
	}
	var output profileOutput
	if err := d.invoke(ctx, StructuredInvocation{
		OperationID:  operationID,
		Stage:        string(memory.OperationStageProfile),
		Instruction:  DefaultDerivationInstruction + " Synthesize one same-scope long-lived profile.",
		InputJSON:    input,
		OutputSchema: profileOutputSchema,
	}, &output); err != nil {
		return nil, err
	}
	return &output.Output, nil
}
