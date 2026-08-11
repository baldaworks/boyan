package memory

// ActiveProjectionRevision joins an active item to its current canonical
// revision. It intentionally contains metadata and evidence only; payload
// content remains behind the canonical storage port.
type ActiveProjectionRevision struct {
	Item     MemoryItem     `json:"item"`
	Revision MemoryRevision `json:"revision"`
}

// BoundedProjectionView is the rebuildable projection input. Canonical state
// and revisions remain authoritative; every slice is bounded independently.
type BoundedProjectionView struct {
	SchemaVersion   string                     `json:"schema_version"`
	State           ScopeState                 `json:"state"`
	Active          []ActiveProjectionRevision `json:"active,omitempty"`
	ActiveTruncated bool                       `json:"active_truncated,omitempty"`
	Recent          []MemoryRevision           `json:"recent,omitempty"`
	Changed         []MemoryRevision           `json:"changed,omitempty"`
}

// Validate verifies exact scope ownership, canonical record shape, and the
// bounded nature of the materialized view.
func (v BoundedProjectionView) Validate() error {
	if v.SchemaVersion != ProjectionViewSchemaVersion {
		return invalidDerived("projection view schema version is invalid")
	}
	if err := v.State.Validate(); err != nil {
		return err
	}
	if len(v.Active) > MaxCanonicalReadRecords || len(v.Recent) > MaxCanonicalReadRecords || len(v.Changed) > MaxCanonicalReadRecords {
		return limitExceeded("projection view exceeds the canonical read bound")
	}
	seenActive := make(map[string]struct{}, len(v.Active))
	for _, active := range v.Active {
		if err := active.Item.Validate(); err != nil {
			return err
		}
		if active.Item.Scope != v.State.Scope || active.Revision.ItemID != active.Item.ItemID || active.Revision.RevisionID == "" {
			return PermanentError(CodeScopeViolation, "projection active revision does not match the scope", nil)
		}
		if err := active.Revision.Validate(); err != nil {
			return err
		}
		if _, exists := seenActive[active.Item.ItemID]; exists {
			return invalidDerived("projection view contains duplicate active items")
		}
		seenActive[active.Item.ItemID] = struct{}{}
	}
	for _, revision := range append(append([]MemoryRevision(nil), v.Recent...), v.Changed...) {
		if err := revision.Validate(); err != nil {
			return err
		}
	}
	return nil
}
