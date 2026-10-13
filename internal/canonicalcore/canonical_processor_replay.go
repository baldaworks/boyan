package canonicalcore

import (
	"context"

	. "github.com/baldaworks/boyan/pkg/memory"
)

type canonicalTurnPreflight struct {
	proof  *CanonicalTurnIdentityResult
	replay *CanonicalMutationOutcome
}

// VerifyCanonicalTurn delegates optional authoritative storage certification.
// Portable stores without this capability cannot certify receipt replay.
func (p *CanonicalTurnProcessor) VerifyCanonicalTurn(
	ctx context.Context,
	turn Turn,
) (CanonicalTurnIdentityResult, error) {
	if p == nil || p.store == nil {
		return CanonicalTurnIdentityResult{}, PermanentError(CodeDisabled, "canonical turn processor is unavailable", nil)
	}

	if ctx == nil {
		return CanonicalTurnIdentityResult{}, invalidDerived("canonical turn verification context is required")
	}

	normalized, err := NormalizeCanonicalTurn(turn)
	if err != nil {
		return CanonicalTurnIdentityResult{}, err
	}

	verifier, supported := p.store.(CanonicalTurnIdentityStore)
	if !supported {
		return CanonicalTurnIdentityResult{}, PermanentError(
			CodeConflict,
			"canonical turn identity certification is unavailable",
			nil,
		)
	}

	return verifier.VerifyCanonicalTurn(ctx, normalized)
}

func (p *CanonicalTurnProcessor) preflightCanonicalTurn(
	ctx context.Context,
	turn Turn,
	derivation DerivationRef,
) (canonicalTurnPreflight, error) {
	var result canonicalTurnPreflight

	if verifier, supported := p.store.(CanonicalTurnIdentityStore); supported {
		proof, err := verifier.VerifyCanonicalTurn(ctx, turn)
		if err != nil {
			return result, err
		}

		if proof.Found {
			if err := validateCertifiedTurnRecords(turn, proof); err != nil {
				return result, err
			}
		}

		result.proof = &proof
	}

	replay, found, err := p.lookupVerifiedTurnOperation(ctx, turn, derivation, result.proof)
	if err != nil {
		return result, err
	}

	if found {
		result.replay = &replay

		return result, checkContext(ctx)
	}

	if result.proof != nil && result.proof.Forgotten {
		return result, PermanentError(CodeConflict, "forgotten canonical source cannot be derived again", nil)
	}

	return result, checkContext(ctx)
}

func (p *CanonicalTurnProcessor) lookupVerifiedTurnOperation(ctx context.Context, turn Turn, derivation DerivationRef,
	proof *CanonicalTurnIdentityResult) (CanonicalMutationOutcome, bool, error) {
	reader, supported := p.store.(CanonicalOperationReader)
	if !supported {
		return CanonicalMutationOutcome{}, false, nil
	}

	operationID, err := CanonicalSemanticOperationID(turn.ExportID, derivation)
	if err != nil {
		return CanonicalMutationOutcome{}, false, err
	}

	record, found, err := reader.LoadCanonicalOperation(ctx, turn.Scope, operationID)
	if err != nil || !found {
		return CanonicalMutationOutcome{}, false, err
	}

	if proof == nil || !proof.Found {
		return CanonicalMutationOutcome{}, false, PermanentError(
			CodeConflict,
			"committed canonical turn identity cannot be verified",
			nil,
		)
	}

	if err := record.Validate(); err != nil {
		return CanonicalMutationOutcome{}, false, PermanentError(CodeStoreFailure, "canonical replay record is invalid", err)
	}

	if record.Fingerprint != mustCanonicalFingerprint(turn.ExportID, derivation) {
		return CanonicalMutationOutcome{}, false, PermanentError(CodeConflict, "canonical operation fingerprint differs", nil)
	}

	return record.Outcome, true, nil
}
