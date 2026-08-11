package memory

import "context"

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
