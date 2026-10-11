package canonicalcore

import (
	"context"

	. "github.com/baldaworks/boyan/pkg/memory"
)

// ProcessTurn extracts bounded semantics from one terminal turn and commits
// the source, messages, and derived revisions as one canonical mutation. The
// model supplies only semantic fields and evidence claims; all identities,
// payload references, revision numbers, and scope state are derived here.
func (p *CanonicalTurnProcessor) ProcessTurn(
	ctx context.Context,
	turn Turn,
	derivation DerivationRef,
) (
	CanonicalMutationOutcome,
	error,
) {
	if p == nil || p.store == nil || p.extractor == nil {
		return CanonicalMutationOutcome{}, PermanentError(CodeDisabled, "canonical turn processor is unavailable", nil)
	}

	if ctx == nil {
		return CanonicalMutationOutcome{}, invalidDerived("canonical turn processing context is required")
	}

	turn, err := prepareCanonicalTurn(turn)
	if err != nil {
		return CanonicalMutationOutcome{}, err
	}

	if err := derivation.Validate(); err != nil {
		return CanonicalMutationOutcome{}, err
	}

	if turnTextExceeds(turn, MaxDerivedTurnTextBytes) {
		return CanonicalMutationOutcome{}, limitExceeded("canonical turn text exceeds the derived processing limit")
	}

	if err := checkContext(ctx); err != nil {
		return CanonicalMutationOutcome{}, err
	}

	return p.processPreparedTurn(ctx, turn, derivation)
}

func (p *CanonicalTurnProcessor) processPreparedTurn(
	ctx context.Context,
	turn Turn,
	derivation DerivationRef,
) (CanonicalMutationOutcome, error) {
	scopeState, active, activeView, err := p.loadCanonicalActiveView(ctx, turn.Scope)
	if err != nil {
		return CanonicalMutationOutcome{}, err
	}

	candidates, err := p.extractCanonicalCandidates(ctx, turn, derivation)
	if err != nil {
		return CanonicalMutationOutcome{}, err
	}

	mutation, err := p.buildCanonicalMutation(turn, derivation, scopeState, active, activeView, candidates)
	if err != nil {
		return CanonicalMutationOutcome{}, err
	}

	outcome, err := p.store.ApplyCanonicalMutation(ctx, mutation)
	if err != nil {
		return CanonicalMutationOutcome{}, err
	}

	if err := outcome.Validate(); err != nil {
		return CanonicalMutationOutcome{}, err
	}

	return outcome, nil
}
