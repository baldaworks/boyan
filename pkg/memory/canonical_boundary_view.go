package memory

import (
	"context"
	"time"
)

// CanonicalBoundaryView is a bounded legacy-presentation view backed by
// canonical records. LegacyToCanonical is an adapter map only; canonical IDs
// remain authoritative for writes and provenance.
type CanonicalBoundaryView struct {
	Boundary          BoundarySnapshot
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

	for legacy, parent := range v.Boundary.Parents {
		if canonical, found := v.LegacyToCanonical[legacy]; !found || canonical != parent.CanonicalRef {
			return invalidDerived("canonical boundary parent identity does not match translation")
		}
	}

	return nil
}

// CanonicalBoundaryViewReader is the storage-neutral boundary input port.
type CanonicalBoundaryViewReader interface {
	LoadCanonicalBoundaryView(ctx context.Context, scope Scope) (CanonicalBoundaryView, error)
	LoadCanonicalBoundaryViewAt(ctx context.Context, scope Scope, at time.Time) (CanonicalBoundaryView, error)
}
