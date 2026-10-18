package canonicalsql

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

var (
	_ memory.CanonicalTurnIdentityStore     = (*Owner)(nil)
	_ memory.CanonicalBoundaryIdentityStore = (*Owner)(nil)
)

// VerifyCanonicalTurn checks the full authoritative input in one admitted snapshot.
func (o *Owner) VerifyCanonicalTurn(ctx context.Context, turn memory.Turn) (memory.CanonicalTurnIdentityResult, error) {
	if ctx == nil {
		return memory.CanonicalTurnIdentityResult{}, invalid("nil canonical context")
	}

	if err := ctx.Err(); err != nil {
		return memory.CanonicalTurnIdentityResult{}, err
	}

	normalized, err := memory.NormalizeCanonicalTurn(turn)
	if err != nil {
		return memory.CanonicalTurnIdentityResult{}, err
	}

	var result memory.CanonicalTurnIdentityResult

	err = o.View(ctx, func(ctx context.Context, tx *Tx) error {
		var err error

		result, err = verifyCanonicalTurnTxn(ctx, tx, normalized)

		return err
	})
	if err != nil {
		return memory.CanonicalTurnIdentityResult{}, err
	}

	return result, nil
}

// VerifyCanonicalBoundary checks immutable proof or bounded namespace capability.
func (o *Owner) VerifyCanonicalBoundary(ctx context.Context,
	boundary memory.Boundary) (memory.BoundaryIdentityResult,
	error) {
	if ctx == nil {
		return memory.BoundaryIdentityResult{}, invalid("nil canonical context")
	}

	if err := ctx.Err(); err != nil {
		return memory.BoundaryIdentityResult{}, err
	}

	identity, err := memory.NewCanonicalBoundaryIdentityCommit(boundary)
	if err != nil {
		return memory.BoundaryIdentityResult{}, err
	}

	var result memory.BoundaryIdentityResult

	err = o.View(ctx, func(ctx context.Context, tx *Tx) error {
		var err error

		result, err = verifyCanonicalBoundaryTxn(ctx, tx, identity)

		return err
	})
	if err != nil {
		return memory.BoundaryIdentityResult{}, err
	}

	return result, nil
}

func proofStoreFailure(message string, err error) error {
	return memory.PermanentError(memory.CodeStoreFailure, message, err)
}

func proofChangeKey(scope memory.Scope, sequence uint64) (Key, error) {
	key := Key{Family: ScopeChange, Scope: scope, Parts: [][]byte{Uint64(sequence)}}
	_, _, err := key.values()

	return key, err
}

func proofScopeKey(scope memory.Scope) (Key, error) {
	key := Key{Family: ScopeState, Scope: scope}
	_, _, err := key.values()

	return key, err
}

func loadProofScopeState(ctx context.Context, tx *Tx, scope memory.Scope, key Key) (memory.ScopeState, error) {
	state := memory.ScopeState{SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: scope}

	err := getPrivacyRecord(ctx, tx, key, ScopeState, &state)
	if err != nil && !privacyMissing(err) {
		if code, _, ok := memory.ClassifyError(err); ok && code == memory.CodeInvalidDerived {
			return memory.ScopeState{}, tx.fail(proofStoreFailure("invalid stored canonical scope", err))
		}

		return memory.ScopeState{}, err
	}

	if stampErr := validateScopeStateStamp(ctx, tx, key, err == nil); stampErr != nil {
		return memory.ScopeState{}, stampErr
	}

	if err := state.Validate(); err != nil || state.Scope != scope {
		return memory.ScopeState{}, proofStoreFailure("invalid canonical scope state", err)
	}

	return state, nil
}
