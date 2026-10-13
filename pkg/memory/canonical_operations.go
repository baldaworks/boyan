package memory

import (
	"context"
	"time"
)

// CanonicalOperationRecord is the durable replay record for one canonical
// mutation. It is optional at the portable boundary so application adapters
// can preserve established stage outcomes without consulting an older format.
type CanonicalOperationRecord struct {
	Fingerprint string
	Outcome     CanonicalMutationOutcome
}

// Validate checks the replay record before an adapter exposes its outcome.
func (r CanonicalOperationRecord) Validate() error {
	if !isCanonicalID(r.Fingerprint) {
		return invalidDerived("canonical operation fingerprint is invalid")
	}

	return r.Outcome.Validate()
}

// CanonicalOperationReader exposes one exact-scope canonical replay record.
// A missing record is a normal lookup miss; implementations must not scan a
// scope to answer it.
type CanonicalOperationReader interface {
	LoadCanonicalOperation(ctx context.Context, scope Scope, operationID string) (CanonicalOperationRecord, bool, error)
}

// CanonicalOperationCommitRequest records an idempotent stage that produced no
// memory revision (for example a successful profile skip) while still
// advancing the exact-scope version and replay watermark.
type CanonicalOperationCommitRequest struct {
	BoundaryIdentity     *CanonicalBoundaryIdentityCommit `json:"-"`
	Scope                Scope
	ExpectedScopeVersion uint64
	OperationID          string
	Fingerprint          string
	CommittedAt          time.Time
}

// Validate checks one operation-only mutation request.
func (r CanonicalOperationCommitRequest) Validate() error {
	if err := r.Scope.Validate(); err != nil {
		return err
	}

	if r.BoundaryIdentity != nil {
		if err := r.BoundaryIdentity.Validate(r.Scope); err != nil {
			return err
		}
	}

	if !isCanonicalID(r.OperationID) || !isCanonicalID(r.Fingerprint) ||
		r.ExpectedScopeVersion == ^uint64(0) ||
		r.CommittedAt.IsZero() {
		return invalidDerived("canonical operation commit identity is invalid")
	}

	return nil
}

// CanonicalOperationCommitter durably records an operation-only mutation.
// Implementations must enforce the same operation fingerprint/CAS rules as a
// regular canonical mutation.
type CanonicalOperationCommitter interface {
	CommitCanonicalOperation(
		ctx context.Context,
		request CanonicalOperationCommitRequest,
	) (
		CanonicalMutationOutcome,
		error,
	)
}
