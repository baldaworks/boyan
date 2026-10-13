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
	if p == nil || p.store == nil {
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

	if err := checkContext(ctx); err != nil {
		return CanonicalMutationOutcome{}, err
	}

	preflight, err := p.preflightCanonicalTurn(ctx, turn, derivation)
	if err != nil {
		return CanonicalMutationOutcome{}, err
	}

	if preflight.replay != nil {
		return *preflight.replay, nil
	}

	return p.processPreparedTurn(ctx, turn, derivation, preflight.proof)
}

func (p *CanonicalTurnProcessor) processPreparedTurn(
	ctx context.Context,
	turn Turn,
	derivation DerivationRef,
	proof *CanonicalTurnIdentityResult,
) (CanonicalMutationOutcome, error) {
	if p.extractor == nil {
		return CanonicalMutationOutcome{}, PermanentError(CodeDisabled, "canonical turn extraction is unavailable", nil)
	}

	if turnTextExceeds(turn, MaxDerivedTurnTextBytes) {
		return CanonicalMutationOutcome{}, limitExceeded("canonical turn text exceeds the derived processing limit")
	}

	candidates, err := p.extractCanonicalCandidates(ctx, turn, derivation)
	if err != nil {
		return CanonicalMutationOutcome{}, err
	}

	scopeState, active, activeView, err := p.loadCanonicalActiveView(ctx, turn, candidates)
	if err != nil {
		return CanonicalMutationOutcome{}, err
	}

	mutation, err := p.buildCanonicalMutation(turn, derivation, scopeState, active, activeView, candidates)
	if err != nil {
		return CanonicalMutationOutcome{}, err
	}

	if err := p.bindCanonicalTurnIdentity(turn, &mutation, proof); err != nil {
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
