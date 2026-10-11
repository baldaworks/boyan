package memory

// ProjectionViewRequest controls the bounded canonical records used by a
// materialized projection rebuild or synthesis call.
type ProjectionViewRequest struct {
	Scope             Scope  `json:"scope"`
	ActiveAfterItemID string `json:"active_after_item_id,omitempty"`
	ActiveLimit       uint32 `json:"active_limit"`
	RecentAfter       uint64 `json:"recent_after"`
	RecentLimit       uint32 `json:"recent_limit"`
	ChangedAfter      uint64 `json:"changed_after"`
	ChangedLimit      uint32 `json:"changed_limit"`
	ActiveOnly        bool   `json:"active_only,omitempty"`
}

// Normalize applies package defaults and rejects invalid cursors or bounds.
func (r ProjectionViewRequest) Normalize() (ProjectionViewRequest, error) {
	if err := r.Scope.Validate(); err != nil {
		return ProjectionViewRequest{}, err
	}

	if r.ActiveAfterItemID != "" && !isCanonicalID(r.ActiveAfterItemID) {
		return ProjectionViewRequest{}, invalidDerived("projection active cursor is invalid")
	}

	out := r.withDefaultLimits()

	for _, limit := range []uint32{out.ActiveLimit, out.RecentLimit, out.ChangedLimit} {
		if limit > MaxCanonicalReadRecords {
			return ProjectionViewRequest{}, limitExceeded("projection view limit exceeds the canonical read bound")
		}
	}

	return out, nil
}

func (r ProjectionViewRequest) withDefaultLimits() ProjectionViewRequest {
	out := r
	if out.ActiveLimit == 0 {
		out.ActiveLimit = defaultProjectionActiveLimit
	}

	if out.ActiveOnly {
		out.RecentLimit = 0
		out.ChangedLimit = 0
	} else if out.RecentLimit == 0 {
		out.RecentLimit = defaultProjectionRecentLimit
	}

	if !out.ActiveOnly && out.ChangedLimit == 0 {
		out.ChangedLimit = defaultProjectionChangedLimit
	}

	return out
}
