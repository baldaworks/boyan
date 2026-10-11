package ingestapp

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// ProcessTurn returns the canonical outcome for callers that need the
// committed watermark.  IngestTurn below intentionally exposes only the
// capability required by a delivery worker.
func (p *TurnProcessor) ProcessTurn(ctx context.Context, turn memory.Turn) (memory.CanonicalMutationOutcome, error) {
	if p == nil || p.processor == nil {
		return memory.CanonicalMutationOutcome{}, memory.PermanentError(
			memory.CodeDisabled,
			"canonical turn processor is unavailable",
			nil,
		)
	}

	outcome, err := p.processor.ProcessTurn(ctx, turn, p.derivation)
	if err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	if p.projection != nil {
		if err := p.projection.Sync(ctx, turn.Scope); err != nil {
			return outcome, err
		}
	}

	return outcome, nil
}
