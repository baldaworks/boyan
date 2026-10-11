package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

var _ memory.CanonicalBoundaryViewReader = (*CanonicalReader)(nil)

// LoadCanonicalBoundaryView exposes only a validated bounded compatibility
// view. Canonical IDs remain in LegacyToCanonical for write translation.
func (r *CanonicalReader) LoadCanonicalBoundaryView(
	ctx context.Context,
	scope memory.Scope,
) (memory.CanonicalBoundaryView, error) {
	records, err := r.LoadActiveRecallRecords(ctx, scope, memory.MaxRecallCandidates)
	if err != nil {
		return memory.CanonicalBoundaryView{}, err
	}

	state, err := r.store.LoadScopeState(ctx, scope)
	if err != nil {
		return memory.CanonicalBoundaryView{}, err
	}

	view := memory.CanonicalBoundaryView{
		Snapshot: memory.ScopeSnapshot{
			SchemaVersion: memory.DerivedSchemaVersionV1,
			Scope:         scope,
			Version:       state.Version,
		},
		LegacyToCanonical: make(map[memory.RevisionRef]memory.RevisionRef),
	}
	for _, record := range records {
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
