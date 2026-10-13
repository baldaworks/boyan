package structuredderive

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// SynthesizeScenarios implements boundary scenario synthesis.
func (d *Deriver) SynthesizeScenarios(
	ctx context.Context,
	request memory.ScenarioSynthesisRequest,
) (
	[]memory.ScenarioCandidate,
	error,
) {
	if err := d.available(); err != nil {
		return nil, err
	}

	request.Derivation = normalizeDerivation(request.Derivation)

	var output scenariosOutput
	if err := d.invokeLegacyStage(ctx, request, memory.OperationStageScenarios,
		request.Boundary.ExportID,
		DefaultDerivationInstruction+" Synthesize same-scope topic scenarios from the active view.",
		request.Derivation, &output); err != nil {
		return nil, err
	}

	return output.Output, nil
}

// SynthesizeProfile implements exact-scope profile synthesis.
func (d *Deriver) SynthesizeProfile(
	ctx context.Context,
	request memory.ProfileSynthesisRequest,
) (
	*memory.ProfileCandidate,
	error,
) {
	if err := d.available(); err != nil {
		return nil, err
	}

	request.Derivation = normalizeDerivation(request.Derivation)

	var output profileOutput
	if err := d.invokeLegacyStage(ctx, request, memory.OperationStageProfile,
		request.Boundary.ExportID,
		DefaultDerivationInstruction+" Synthesize one same-scope long-lived profile.",
		request.Derivation, &output); err != nil {
		return nil, err
	}

	return &output.Output, nil
}
