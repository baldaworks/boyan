package memory

import "context"

// CanonicalBoundaryView is a bounded legacy-presentation view backed by
// canonical records. LegacyToCanonical is an adapter map only; canonical IDs
// remain authoritative for writes and provenance.
type CanonicalBoundaryView struct {
	Snapshot          ScopeSnapshot
	LegacyToCanonical map[RevisionRef]RevisionRef
}

// Validate checks one bounded boundary compatibility view.
func (v CanonicalBoundaryView) Validate() error {
	if err := v.Snapshot.Validate(MaxSnapshotItems); err != nil {
		return err
	}
	for legacy, canonical := range v.LegacyToCanonical {
		if err := legacy.Validate(); err != nil {
			return err
		}
		if err := canonical.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// CanonicalBoundaryViewReader is the storage-neutral boundary input port.
type CanonicalBoundaryViewReader interface {
	LoadCanonicalBoundaryView(ctx context.Context, scope Scope) (CanonicalBoundaryView, error)
}
