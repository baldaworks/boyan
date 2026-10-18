package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func buildCanonicalBoundaryView(ctx context.Context,
	scope memory.Scope,
	version uint64,
	records []memory.RecallRecord) (memory.CanonicalBoundaryView,
	error) {
	view := memory.CanonicalBoundaryView{
		Snapshot: memory.ScopeSnapshot{
			SchemaVersion: memory.DerivedSchemaVersionV1,
			Scope:         scope,
			Version:       version,
		},
		LegacyToCanonical: make(map[memory.RevisionRef]memory.RevisionRef),
	}

	for _, record := range records {
		if err := mutationContext(ctx); err != nil {
			return memory.CanonicalBoundaryView{}, err
		}

		entry, err := canonicalBoundaryEntry(scope, record)
		if err != nil {
			return memory.CanonicalBoundaryView{}, err
		}

		switch {
		case entry.atom != nil:
			view.Snapshot.Atoms = append(view.Snapshot.Atoms, *entry.atom)
		case entry.scenario != nil:
			view.Snapshot.Scenarios = append(view.Snapshot.Scenarios, *entry.scenario)
		case entry.profile != nil:
			view.Snapshot.Profiles = append(view.Snapshot.Profiles, *entry.profile)
		}

		view.LegacyToCanonical[entry.legacyRef] = memory.RevisionRef{ItemID: record.ItemID, RevisionID: record.RevisionID}
	}

	if err := view.Validate(); err != nil {
		return memory.CanonicalBoundaryView{}, err
	}

	return view, nil
}
