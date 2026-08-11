package memory

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
