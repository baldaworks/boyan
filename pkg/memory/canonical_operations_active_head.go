package memory

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
