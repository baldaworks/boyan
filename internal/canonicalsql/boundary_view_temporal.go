package canonicalsql

import (
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func temporalCanonicalBoundaryView(view memory.CanonicalBoundaryView,
	records []memory.RecallRecord, at time.Time) (memory.CanonicalBoundaryView, error) {
	parents := make(map[memory.RevisionRef]memory.BoundaryParentMetadata, len(records))

	eligible := make(map[memory.RevisionRef]bool, len(records))
	for _, record := range records {
		entry, err := canonicalBoundaryEntry(view.Snapshot.Scope, record)
		if err != nil {
			return memory.CanonicalBoundaryView{}, err
		}

		parents[entry.legacyRef] = memory.BoundaryParentMetadata{
			CanonicalRef: memory.RevisionRef{ItemID: record.ItemID, RevisionID: record.RevisionID},
			Temporal:     cloneBoundaryTemporal(record.Temporal),
			Sensitivity:  record.Sensitivity,
			Retention:    record.Retention,
		}
		eligible[entry.legacyRef] = memory.TemporalEligibleAt(record.Temporal, at) &&
			record.Sensitivity == memory.SensitivityStandard
	}

	view.Boundary = memory.BoundarySnapshot{
		Structural: view.Snapshot,
		Eligible: memory.ScopeView{SchemaVersion: view.Snapshot.SchemaVersion,
			Scope: view.Snapshot.Scope, Version: view.Snapshot.Version},
		Parents: parents,
	}
	populateEligibleBoundaryView(&view.Boundary, eligible)

	if err := view.Boundary.Validate(memory.MaxSnapshotItems, at); err != nil {
		return memory.CanonicalBoundaryView{}, err
	}

	if err := view.Validate(); err != nil {
		return memory.CanonicalBoundaryView{}, err
	}

	return view, nil
}

func populateEligibleBoundaryView(snapshot *memory.BoundarySnapshot, eligible map[memory.RevisionRef]bool) {
	for _, atom := range snapshot.Structural.Atoms {
		ref := memory.RevisionRef{ItemID: atom.Meta.ItemID, RevisionID: atom.Meta.RevisionID}
		if eligible[ref] {
			temporal := cloneBoundaryTemporal(snapshot.Parents[ref].Temporal)
			atom.Temporal = &temporal
			snapshot.Eligible.Atoms = append(snapshot.Eligible.Atoms, atom)
		}
	}

	for _, scenario := range snapshot.Structural.Scenarios {
		ref := memory.RevisionRef{ItemID: scenario.Meta.ItemID, RevisionID: scenario.Meta.RevisionID}
		if eligible[ref] {
			temporal := cloneBoundaryTemporal(snapshot.Parents[ref].Temporal)
			scenario.Temporal = &temporal
			snapshot.Eligible.Scenarios = append(snapshot.Eligible.Scenarios, scenario)
		}
	}

	for _, profile := range snapshot.Structural.Profiles {
		ref := memory.RevisionRef{ItemID: profile.Meta.ItemID, RevisionID: profile.Meta.RevisionID}
		if eligible[ref] {
			temporal := cloneBoundaryTemporal(snapshot.Parents[ref].Temporal)
			profile.Temporal = &temporal
			snapshot.Eligible.Profiles = append(snapshot.Eligible.Profiles, profile)
		}
	}
}
