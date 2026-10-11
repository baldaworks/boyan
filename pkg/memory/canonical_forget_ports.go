package memory

import (
	"context"
	"time"
)

// CanonicalSourceForgetStore is the narrow persistence port required for one
// resumable logical-forget batch. Implementations persist records and bounded
// provenance scans only; they do not choose cascade order or retry policy.
type CanonicalSourceForgetStore interface {
	DenySource(ctx context.Context, scope Scope, sourceID string, deniedAt time.Time) error
	SourceRevisionBatch(
		ctx context.Context,
		scope Scope,
		sourceID, afterRevisionID string,
		limit uint32,
	) (
		[]string,
		string,
		error,
	)
	DenyRevision(ctx context.Context, scope Scope, revisionID string, deniedAt time.Time) error
}

// CanonicalForgetEnumerator is the bounded exact-scope enumeration port used
// by a scope forget. Implementations return deterministic cursors and never
// expose payload bytes.
type CanonicalForgetEnumerator interface {
	ListCanonicalSourceRefs(
		ctx context.Context,
		scope Scope,
		afterSourceID string,
		limit uint32,
	) (
		[]SourceRef,
		string,
		error,
	)
	ListCanonicalRevisionRefs(
		ctx context.Context,
		scope Scope,
		afterRevisionID string,
		limit uint32,
	) (
		[]RevisionRef,
		string,
		error,
	)
}

// CanonicalSourceIdentityResolver maps a public source reference to its
// canonical evidence identity. Migration adapters use this to preserve the
// original export identity while native turns use TurnSourceID.
type CanonicalSourceIdentityResolver interface {
	CanonicalSourceID(ctx context.Context, scope Scope, source SourceRef) (string, error)
}

// CanonicalForgetOutcomeStore persists idempotent forget outcomes separately
// from immutable memory payloads. It is optional for portable callers, but a
// production canonical owner implements it so retries return the exact prior
// outcome.
type CanonicalForgetOutcomeStore interface {
	LoadCanonicalForgetOutcome(
		ctx context.Context,
		scope Scope,
		operationID string,
		kind ForgetKind,
	) (
		ForgetOutcome,
		bool,
		error,
	)
	CommitCanonicalForget(ctx context.Context, request CanonicalForgetCommitRequest) (ForgetOutcome, error)
}
