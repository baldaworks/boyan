package memory

import "context"

// CanonicalScopeStatePager optionally enumerates durable scope cursors, including
// scopes that have never acquired a projection manifest. The cursor is opaque,
// exclusive and restart-safe; nil next marks the final page. Implementations
// bound each page by MaxCanonicalReadRecords. Concurrent new scopes are repaired
// by their normal mutation sync; enumeration does not provide a global snapshot.
type CanonicalScopeStatePager interface {
	ListCanonicalScopeStates(ctx context.Context, cursor []byte, limit int) ([]ScopeState, []byte, error)
}
