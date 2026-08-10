package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// LoadActiveRecallRecords returns a bounded active-head page suitable for the
// boundary compatibility adapter. It deliberately has no query parameter and
// never walks historical revisions.
func (r *CanonicalReader) LoadActiveRecallRecords(ctx context.Context, scope memory.Scope, limit uint32) ([]memory.RecallRecord, error) {
	if err := canonicalReaderContext(ctx); err != nil {
		return nil, err
	}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if limit == 0 || limit > memory.MaxRecallCandidates {
		return nil, memory.PermanentError(memory.CodeLimitExceeded, "active canonical recall limit is invalid", nil)
	}
	active, err := r.store.ScanActiveMemory(ctx, memory.ActiveMemoryScanRequest{Scope: scope, Limit: limit})
	if err != nil {
		return nil, err
	}
	if len(active) == int(limit) {
		probe, probeErr := r.store.ScanActiveMemory(ctx, memory.ActiveMemoryScanRequest{Scope: scope, AfterItemID: active[len(active)-1].Item.ItemID, Limit: 1})
		if probeErr != nil {
			return nil, probeErr
		}
		if len(probe) != 0 {
			return nil, memory.PermanentError(memory.CodeLimitExceeded, "canonical boundary view exceeds the bounded active limit", nil)
		}
	}
	result := make([]memory.RecallRecord, 0, len(active))
	for _, memory := range active {
		record, found, loadErr := r.loadRecallRecord(ctx, scope, memory.RevisionID, true)
		if loadErr != nil {
			return nil, loadErr
		}
		if found {
			result = append(result, record)
		}
	}
	return result, nil
}

var _ memory.CanonicalBoundaryViewReader = (*CanonicalReader)(nil)

// LoadCanonicalCompatibility resolves one exact canonical revision's
// rebuildable legacy presentation metadata, including superseded revisions.
// It is used only for idempotent boundary-operation replay; active reads stay
// on LoadActiveRecallRecords and never widen their bounded view.
func (r *CanonicalReader) LoadCanonicalCompatibility(ctx context.Context, scope memory.Scope, revisionID string) (memory.CanonicalCompatibilityPayload, bool, error) {
	if err := canonicalReaderContext(ctx); err != nil {
		return memory.CanonicalCompatibilityPayload{}, false, err
	}
	if err := scope.Validate(); err != nil {
		return memory.CanonicalCompatibilityPayload{}, false, err
	}
	view, found, err := r.loadCanonicalView(ctx, scope, revisionID, false)
	if err != nil || !found {
		return memory.CanonicalCompatibilityPayload{}, found, err
	}
	payload, err := r.store.LoadPayload(ctx, view.revision.Payload)
	if err != nil {
		return memory.CanonicalCompatibilityPayload{}, false, err
	}
	compatibility, ok := canonicalCompatibilityPayload(payload, scope)
	return compatibility, ok, nil
}

// LoadCanonicalBoundaryView exposes only a validated bounded compatibility
// view. Canonical IDs remain in LegacyToCanonical for write translation.
func (r *CanonicalReader) LoadCanonicalBoundaryView(ctx context.Context, scope memory.Scope) (memory.CanonicalBoundaryView, error) {
	records, err := r.LoadActiveRecallRecords(ctx, scope, memory.MaxRecallCandidates)
	if err != nil {
		return memory.CanonicalBoundaryView{}, err
	}
	state, err := r.store.LoadScopeState(ctx, scope)
	if err != nil {
		return memory.CanonicalBoundaryView{}, err
	}
	view := memory.CanonicalBoundaryView{Snapshot: memory.ScopeSnapshot{SchemaVersion: memory.DerivedSchemaVersionV1, Scope: scope, Version: state.Version}, LegacyToCanonical: make(map[memory.RevisionRef]memory.RevisionRef)}
	for _, record := range records {
		provenance := memory.Provenance{ParentRevisions: append([]memory.RevisionRef(nil), record.LegacyParents...)}
		if record.LegacyKind == nil || *record.LegacyKind == memory.DerivedKindAtom {
			provenance.RawSources = append(provenance.RawSources, record.SourceRefs...)
		}
		if err := provenance.Validate(scope); err != nil {
			return memory.CanonicalBoundaryView{}, err
		}
		kind := memory.DerivedKindAtom
		if record.LegacyKind != nil {
			kind = *record.LegacyKind
		}
		operationStage := memory.OperationStageAtoms
		if kind == memory.DerivedKindScenario {
			operationStage = memory.OperationStageScenarios
		}
		if kind == memory.DerivedKindProfile {
			operationStage = memory.OperationStageProfile
		}
		operationID := record.LegacyOperationID
		if operationID == "" {
			var opErr error
			operationID, opErr = memory.ProcessingOperationID(operationStage, record.RevisionID, memory.LegacyDerivationRef())
			if opErr != nil {
				return memory.CanonicalBoundaryView{}, opErr
			}
		}
		legacyRef := memory.RevisionRef{}
		switch kind {
		case memory.DerivedKindAtom:
			category := memory.AtomCategoryFact
			if record.Category != nil {
				category = *record.Category
			}
			itemID := record.LegacyItemID
			if itemID == "" {
				var idErr error
				itemID, idErr = memory.AtomItemID(scope, category, record.Text)
				if idErr != nil {
					return memory.CanonicalBoundaryView{}, idErr
				}
			}
			meta := memory.RevisionMeta{SchemaVersion: memory.DerivedSchemaVersionV1, Kind: kind, ItemID: itemID, Revision: record.Revision, OperationID: operationID, Scope: scope, State: memory.RevisionStateActive, Provenance: provenance, CreatedAt: record.CreatedAt}
			revisionID := record.LegacyRevisionID
			if revisionID == "" {
				var idErr error
				revisionID, idErr = memory.DerivedRevisionID(scope, itemID, operationID, []string{string(category), record.Text, string(memory.CandidateRelationNew)}, provenance, nil)
				if idErr != nil {
					return memory.CanonicalBoundaryView{}, idErr
				}
			}
			meta.RevisionID = revisionID
			atom := memory.Atom{Meta: meta, Category: category, Text: record.Text, Relation: memory.CandidateRelationNew}
			if err := atom.Validate(); err != nil {
				return memory.CanonicalBoundaryView{}, err
			}
			view.Snapshot.Atoms = append(view.Snapshot.Atoms, atom)
			legacyRef = memory.RevisionRef{ItemID: itemID, RevisionID: revisionID}
		case memory.DerivedKindScenario:
			itemID := record.LegacyItemID
			if itemID == "" {
				var idErr error
				itemID, idErr = memory.ScenarioItemID(scope, record.TopicKey)
				if idErr != nil {
					return memory.CanonicalBoundaryView{}, idErr
				}
			}
			meta := memory.RevisionMeta{SchemaVersion: memory.DerivedSchemaVersionV1, Kind: kind, ItemID: itemID, Revision: record.Revision, OperationID: operationID, Scope: scope, State: memory.RevisionStateActive, Provenance: provenance, CreatedAt: record.CreatedAt}
			revisionID := record.LegacyRevisionID
			if revisionID == "" {
				var idErr error
				revisionID, idErr = memory.DerivedRevisionID(scope, itemID, operationID, []string{record.TopicKey, record.Title, record.Text}, provenance, nil)
				if idErr != nil {
					return memory.CanonicalBoundaryView{}, idErr
				}
			}
			if record.LegacySupersedes != nil {
				copyOf := *record.LegacySupersedes
				meta.Supersedes = &copyOf
			}
			meta.RevisionID = revisionID
			scenario := memory.Scenario{Meta: meta, TopicKey: record.TopicKey, Title: record.Title, Summary: record.Text}
			if err := scenario.Validate(); err != nil {
				return memory.CanonicalBoundaryView{}, err
			}
			view.Snapshot.Scenarios = append(view.Snapshot.Scenarios, scenario)
			legacyRef = memory.RevisionRef{ItemID: itemID, RevisionID: revisionID}
		case memory.DerivedKindProfile:
			itemID := record.LegacyItemID
			if itemID == "" {
				var idErr error
				itemID, idErr = memory.ProfileItemID(scope)
				if idErr != nil {
					return memory.CanonicalBoundaryView{}, idErr
				}
			}
			meta := memory.RevisionMeta{SchemaVersion: memory.DerivedSchemaVersionV1, Kind: kind, ItemID: itemID, Revision: record.Revision, OperationID: operationID, Scope: scope, State: memory.RevisionStateActive, Provenance: provenance, CreatedAt: record.CreatedAt}
			revisionID := record.LegacyRevisionID
			if revisionID == "" {
				var idErr error
				revisionID, idErr = memory.DerivedRevisionID(scope, itemID, operationID, []string{record.Text}, provenance, nil)
				if idErr != nil {
					return memory.CanonicalBoundaryView{}, idErr
				}
			}
			if record.LegacySupersedes != nil {
				copyOf := *record.LegacySupersedes
				meta.Supersedes = &copyOf
			}
			meta.RevisionID = revisionID
			profile := memory.Profile{Meta: meta, Summary: record.Text}
			if err := profile.Validate(); err != nil {
				return memory.CanonicalBoundaryView{}, err
			}
			view.Snapshot.Profiles = append(view.Snapshot.Profiles, profile)
			legacyRef = memory.RevisionRef{ItemID: itemID, RevisionID: revisionID}
		}
		view.LegacyToCanonical[legacyRef] = memory.RevisionRef{ItemID: record.ItemID, RevisionID: record.RevisionID}
	}
	if err := view.Validate(); err != nil {
		return memory.CanonicalBoundaryView{}, err
	}
	return view, nil
}
