package structuredderive

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// ExtractAtoms implements the legacy atom model port while hosts migrate to
// canonical semantic extraction. It remains here as a pure application
// adapter; the host-specific provider is only a StructuredInvoker.
func (d *Deriver) ExtractAtoms(
	ctx context.Context,
	request memory.AtomExtractionRequest,
) (
	[]memory.AtomCandidate,
	error,
) {
	if err := d.available(); err != nil {
		return nil, err
	}

	request.Derivation = normalizeDerivation(request.Derivation)

	var output atomsOutput
	if err := d.invokeLegacyStage(ctx, request, memory.OperationStageAtoms,
		request.Turn.ExportID,
		DefaultDerivationInstruction+" Extract only grounded atom candidates from this completed turn.",
		request.Derivation, &output); err != nil {
		return nil, err
	}

	return output.Output, nil
}
