package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"strings"
	"time"
)

type canonicalForgetOutcome struct {
	Fingerprint string               `json:"fingerprint"`
	Outcome     memory.ForgetOutcome `json:"outcome"`
}

func forgetOutcomeKey(scope memory.Scope, kind memory.ForgetKind, id string) (Key, error) {
	if err := validateForgetOutcomeLookup(kind, id); err != nil {
		return Key{}, err
	}

	return mutationEdgeKey(scope, ForgetOutcome, string(kind), id)
}
func validateForgetOutcomeLookup(kind memory.ForgetKind, id string) error {
	if kind.Validate() != nil || strings.TrimSpace(id) != id || id == "" {
		return memory.PermanentError(memory.CodeInvalidDerived, "canonical forget outcome lookup is invalid", nil)
	}

	return nil
}
func validateStoredForgetOutcome(out memory.ForgetOutcome, scope memory.Scope, id string,
	kind memory.ForgetKind) error {
	if out.Validate() != nil || out.Scope != scope || out.OperationID != id || out.Kind != kind {
		return corruptRow()
	}

	return nil
}

// LoadCanonicalForgetOutcome returns the original durable result.
func (o *Owner) LoadCanonicalForgetOutcome(ctx context.Context, scope memory.Scope, id string,
	kind memory.ForgetKind) (memory.ForgetOutcome, bool, error) {
	key, err := forgetOutcomeKey(scope, kind, id)
	if err != nil {
		return memory.ForgetOutcome{}, false, err
	}

	var stored canonicalForgetOutcome

	found := false

	err = o.View(ctx, func(ctx context.Context, tx *Tx) error {
		err := getPrivacyRecord(ctx, tx, key, ForgetOutcome, &stored)
		if privacyMissing(err) {
			return nil
		}

		if err != nil {
			return err
		}

		if err := validateStoredForgetOutcome(stored.Outcome, scope, id, kind); err != nil {
			return err
		}

		found = true

		return nil
	})
	if err != nil {
		return memory.ForgetOutcome{}, false, err
	}

	return stored.Outcome, found, nil
}

// CommitCanonicalForget commits only one outcome and scope change, preserving replay time.
func (o *Owner) CommitCanonicalForget(ctx context.Context,
	request memory.CanonicalForgetCommitRequest) (memory.ForgetOutcome, error) {
	if err := mutationContext(ctx); err != nil {
		return memory.ForgetOutcome{}, err
	}

	if err := request.Validate(); err != nil {
		return memory.ForgetOutcome{}, err
	}

	key, err := forgetOutcomeKey(request.Scope, request.Kind, request.OperationID)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}

	scopeKey, err := proofScopeKey(request.Scope)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}

	var out memory.ForgetOutcome

	err = o.Update(ctx, func(ctx context.Context, tx *Tx) error {
		var err error

		out, err = commitForgetTxn(ctx, tx, request, key, scopeKey)

		return err
	})
	if err != nil {
		return memory.ForgetOutcome{}, err
	}

	return out, nil
}
func advanceForgetState(state memory.ScopeState) (memory.ScopeState, error) {
	var err error

	state.Version, err = NextUint64(state.Version)
	if err != nil {
		return state, err
	}

	state.ChangeSeq, err = NextUint64(state.ChangeSeq)

	return state, err
}
func writeCanonicalForgetOutcome(ctx context.Context, tx *Tx, request memory.CanonicalForgetCommitRequest,
	key, scopeKey Key, state memory.ScopeState, out memory.ForgetOutcome) error {
	changeKey, err := proofChangeKey(request.Scope, state.ChangeSeq)
	if err != nil {
		return err
	}

	change := memory.ScopeChange{RequiresRebuild: out.ReferencesTruncated, Sequence: state.ChangeSeq,
		ScopeVersion: state.Version,
		OperationID:  request.OperationID, OccurredAt: request.ForgottenAt.UTC()}
	for _, ref := range request.Revisions {
		change.RevisionIDs = append(change.RevisionIDs, ref.RevisionID)
	}

	if err := putPrivacyRecord(ctx, tx, changeKey, ScopeChange, change); err != nil {
		return err
	}

	if err := putPrivacyRecord(ctx, tx, scopeKey, ScopeState, state); err != nil {
		return err
	}

	return putPrivacyRecord(ctx, tx, key, ForgetOutcome,
		canonicalForgetOutcome{Fingerprint: request.Fingerprint, Outcome: out})
}

// DenySource persists a permanent source restriction before asynchronous work.
func (o *Owner) DenySource(ctx context.Context, scope memory.Scope, id string, at time.Time) error {
	if strings.TrimSpace(id) != id {
		return memory.PermanentError(memory.CodeInvalidDerived, "source deny request is invalid", nil)
	}

	return o.deny(ctx, scope, DeniedSource, id, at)
}

// DenyRevision persists a permanent revision restriction.
func (o *Owner) DenyRevision(ctx context.Context, scope memory.Scope, id string, at time.Time) error {
	return o.deny(ctx, scope, DeniedRevision, id, at)
}
func (o *Owner) deny(ctx context.Context, scope memory.Scope, family Family, id string, at time.Time) error {
	if err := mutationContext(ctx); err != nil {
		return err
	}

	if id == "" || at.IsZero() {
		return memory.PermanentError(memory.CodeInvalidDerived, "canonical deny request is invalid", nil)
	}

	key, err := privacyKey(scope, family, id)
	if err != nil {
		return err
	}

	var value any = deniedSource{SourceID: id, DeniedAt: at.UTC()}
	if family == DeniedRevision {
		value = deniedRevision{RevisionID: id, DeniedAt: at.UTC()}
	}

	return o.Update(ctx, func(ctx context.Context, tx *Tx) error {
		if err := checkScopeEraseFence(ctx, tx, scope, ""); err != nil {
			return err
		}

		return putDenyRecord(ctx, tx, key, family, value)
	})
}
func putDenyRecord(ctx context.Context, tx *Tx, key Key, family Family, value any) error {
	found, err := privacyDenied(ctx, tx, key.Scope, family, string(key.Parts[0]))
	if err != nil || found {
		return err
	}

	return putPrivacyRecord(ctx, tx, key, family, value)
}

func commitForgetTxn(ctx context.Context, tx *Tx, request memory.CanonicalForgetCommitRequest, key,
	scopeKey Key) (memory.ForgetOutcome, error) {
	if err := checkScopeEraseFence(ctx, tx, request.Scope, ""); err != nil {
		return memory.ForgetOutcome{}, err
	}

	if out, found, err := replayForgetTxn(ctx, tx, request, key); found || err != nil {
		return out, err
	}

	return writeNewForgetTxn(ctx, tx, request, key, scopeKey)
}
func replayForgetTxn(ctx context.Context, tx *Tx, request memory.CanonicalForgetCommitRequest,
	key Key) (memory.ForgetOutcome, bool, error) {
	var stored canonicalForgetOutcome

	err := getPrivacyRecord(ctx, tx, key, ForgetOutcome, &stored)
	if privacyMissing(err) {
		return memory.ForgetOutcome{}, false, nil
	}

	if err != nil {
		return memory.ForgetOutcome{}, false, err
	}

	if stored.Fingerprint != request.Fingerprint {
		return memory.ForgetOutcome{}, true, memory.PermanentError(memory.CodeConflict,
			"canonical forget operation identity was reused", nil)
	}

	if err := validateStoredForgetOutcome(stored.Outcome, request.Scope, request.OperationID, request.Kind); err != nil {
		return memory.ForgetOutcome{}, true, err
	}

	return stored.Outcome, true, nil
}
func writeNewForgetTxn(ctx context.Context, tx *Tx, request memory.CanonicalForgetCommitRequest, key,
	scopeKey Key) (memory.ForgetOutcome, error) {
	state, err := loadProofScopeState(ctx, tx, request.Scope, scopeKey)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}

	if state.Version != request.ExpectedScopeVersion {
		return memory.ForgetOutcome{}, eraseConflict()
	}

	state, err = advanceForgetState(state)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}

	out := memory.ForgetOutcome{SchemaVersion: memory.DerivedSchemaVersionV1, OperationID: request.OperationID,
		Kind: request.Kind, Scope: request.Scope, ForgottenAt: request.ForgottenAt.UTC(), ScopeVersion: state.Version,
		Sources: append([]memory.SourceRef(nil), request.Sources...), Revisions: append([]memory.RevisionRef(nil),
			request.Revisions...)}
	if err := out.Validate(); err != nil {
		return memory.ForgetOutcome{}, err
	}

	err = writeCanonicalForgetOutcome(ctx, tx, request, key, scopeKey, state, out)

	return out, err
}
