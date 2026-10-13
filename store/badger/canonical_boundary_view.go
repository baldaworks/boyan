package badger

import (
	"context"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

var _ memory.CanonicalBoundaryViewReader = (*CanonicalReader)(nil)

// LoadCanonicalBoundaryView exposes only a validated bounded compatibility
// view. Canonical IDs remain in LegacyToCanonical for write translation.
func (r *CanonicalReader) LoadCanonicalBoundaryView(
	ctx context.Context,
	scope memory.Scope,
) (memory.CanonicalBoundaryView, error) {
	return r.loadCanonicalBoundaryView(ctx, scope, nil)
}

// LoadCanonicalBoundaryViewAt separates bounded structural heads from factual
// context eligible at the single processing instant chosen by the caller.
func (r *CanonicalReader) LoadCanonicalBoundaryViewAt(ctx context.Context,
	scope memory.Scope, at time.Time) (memory.CanonicalBoundaryView, error) {
	if at.IsZero() {
		return memory.CanonicalBoundaryView{}, memory.PermanentError(memory.CodeInvalidDerived,
			"boundary processing time is required", nil)
	}

	return r.loadCanonicalBoundaryView(ctx, scope, &at)
}

func (r *CanonicalReader) loadCanonicalBoundaryView(ctx context.Context,
	scope memory.Scope, at *time.Time) (memory.CanonicalBoundaryView, error) {
	if err := canonicalReaderContext(ctx); err != nil {
		return memory.CanonicalBoundaryView{}, err
	}

	if !r.hasOpenStore() {
		return memory.CanonicalBoundaryView{},
			memory.PermanentError(memory.CodeStoreFailure,
				"canonical reader is closed",
				nil)
	}

	if err := scope.Validate(); err != nil {
		return memory.CanonicalBoundaryView{}, err
	}

	state, err := r.store.LoadScopeState(ctx, scope)
	if err != nil {
		return memory.CanonicalBoundaryView{}, err
	}

	records, err := collectBoundaryRecallRecords(ctx, scope, r.store, r.loadActiveRecallRecords)
	if err != nil {
		return memory.CanonicalBoundaryView{}, err
	}

	if at != nil {
		records, err = r.resolveBoundaryRecordBounds(ctx, scope, records, *at)
		if err != nil {
			return memory.CanonicalBoundaryView{}, err
		}
	}

	view, err := buildCanonicalBoundaryView(ctx, scope, state.Version, records)
	if err != nil || at == nil {
		return view, err
	}

	return temporalCanonicalBoundaryView(view, records, *at)
}

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
		if err := canonicalReaderContext(ctx); err != nil {
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
