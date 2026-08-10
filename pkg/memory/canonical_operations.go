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
	if !isCanonicalID(r.OperationID) || !isCanonicalID(r.Fingerprint) || r.ExpectedScopeVersion == ^uint64(0) || r.CommittedAt.IsZero() {
		return invalidDerived("canonical operation commit identity is invalid")
	}
	return nil
}

// CanonicalOperationCommitter durably records an operation-only mutation.
// Implementations must enforce the same operation fingerprint/CAS rules as a
// regular canonical mutation.
type CanonicalOperationCommitter interface {
	CommitCanonicalOperation(ctx context.Context, request CanonicalOperationCommitRequest) (CanonicalMutationOutcome, error)
}

// CanonicalRevisionReadRequest hydrates a bounded, exact-scope set of
// immutable revisions. Callers provide IDs from a change log or a bounded
// projection candidate set; implementations must not scan a whole scope.
type CanonicalRevisionReadRequest struct {
	Scope       Scope
	RevisionIDs []string
}

// Validate checks one bounded revision hydration request.
func (r CanonicalRevisionReadRequest) Validate() error {
	if err := r.Scope.Validate(); err != nil {
		return err
	}
	if len(r.RevisionIDs) == 0 || len(r.RevisionIDs) > MaxCanonicalReadRecords {
		return limitExceeded("canonical revision read size is invalid")
	}
	seen := make(map[string]struct{}, len(r.RevisionIDs))
	for _, id := range r.RevisionIDs {
		if !isCanonicalID(id) {
			return invalidDerived("canonical revision id is invalid")
		}
		if _, ok := seen[id]; ok {
			return invalidDerived("canonical revision read contains duplicates")
		}
		seen[id] = struct{}{}
	}
	return nil
}

// ActiveHeadScanRequest pages the mutable active-head index in deterministic
// item-ID order. It is the bounded rebuild primitive for projections.
type ActiveHeadScanRequest struct {
	Scope       Scope
	AfterItemID string
	Limit       uint32
}

// Validate checks one bounded active-head scan request.
func (r ActiveHeadScanRequest) Validate() error {
	if err := r.Scope.Validate(); err != nil {
		return err
	}
	if r.AfterItemID != "" && !isCanonicalID(r.AfterItemID) {
		return invalidDerived("canonical active-head cursor is invalid")
	}
	if r.Limit == 0 || r.Limit > MaxCanonicalReadRecords {
		return limitExceeded("canonical active-head scan limit is invalid")
	}
	return nil
}

// ActiveMemoryScanRequest pages active item metadata and the evidence of each
// active revision in deterministic item-ID order. It is the bounded context
// read used by the reconciler; implementations must not load full history.
type ActiveMemoryScanRequest struct {
	Scope       Scope
	AfterItemID string
	Limit       uint32
}

// Validate checks one bounded active-memory scan request.
func (r ActiveMemoryScanRequest) Validate() error {
	return ActiveHeadScanRequest(r).Validate()
}

// ActiveCanonicalMemory joins an active item to its current revision evidence.
// It intentionally excludes payload content and other historic revisions.
type ActiveCanonicalMemory struct {
	Item       MemoryItem
	RevisionID string
	Revision   uint64
	Evidence   []EvidenceRef
}

// CanonicalStore is the storage-neutral incremental persistence port.
// Implementations must atomically enforce ScopeState CAS and operation
// fingerprint replay, and must not read or rewrite full scope history to
// commit one mutation.
type CanonicalStore interface {
	LoadScopeState(ctx context.Context, scope Scope) (ScopeState, error)
	ApplyCanonicalMutation(ctx context.Context, mutation CanonicalMutation) (CanonicalMutationOutcome, error)
	ScanScopeChanges(ctx context.Context, scope Scope, after uint64, limit uint32) ([]ScopeChange, error)
	LoadCanonicalRevisions(ctx context.Context, request CanonicalRevisionReadRequest) ([]MemoryRevision, error)
	ScanActiveHeads(ctx context.Context, request ActiveHeadScanRequest) ([]ItemHead, error)
	ScanActiveMemory(ctx context.Context, request ActiveMemoryScanRequest) ([]ActiveCanonicalMemory, error)
	ClaimDeliveryOutbox(ctx context.Context, request DeliveryClaimRequest) ([]ClaimedDelivery, error)
	SettleDeliveryOutbox(ctx context.Context, request DeliverySettlementRequest) error
}
