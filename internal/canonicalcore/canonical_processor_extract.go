package canonicalcore

import (
	"context"

	. "github.com/baldaworks/boyan/pkg/memory"
)

func (p *CanonicalTurnProcessor) extractCanonicalCandidates(ctx context.Context, turn Turn, derivation DerivationRef) ([]SemanticCandidate, error) {
	candidates, err := p.extractor.ExtractCanonicalSemantics(ctx, CanonicalExtractionRequest{
		SchemaVersion: CanonicalSchemaVersionV1,
		Derivation:    derivation,
		Turn:          cloneTurn(turn),
	})
	if err != nil {
		return nil, err
	}
	if len(candidates) > MaxCandidateCount {
		return nil, limitExceeded("canonical semantic candidate count exceeds the limit")
	}
	return candidates, nil
}
