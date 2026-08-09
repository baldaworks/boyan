package badger

import (
	"context"

	"github.com/baldaworks/boyan"
)

// LoadActiveRecallRecords returns a bounded active-head page suitable for the
// boundary compatibility adapter. It deliberately has no query parameter and
// never walks historical revisions.
func (r *CanonicalReader) LoadActiveRecallRecords(ctx context.Context, scope boyan.Scope, limit uint32) ([]boyan.RecallRecord, error) {
	if err := canonicalReaderContext(ctx); err != nil {
		return nil, err
	}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if limit == 0 || limit > boyan.MaxRecallCandidates {
		return nil, boyan.PermanentError(boyan.CodeLimitExceeded, "active canonical recall limit is invalid", nil)
	}
	active, err := r.store.ScanActiveMemory(ctx, boyan.ActiveMemoryScanRequest{Scope: scope, Limit: limit})
	if err != nil {
		return nil, err
	}
	if len(active) == int(limit) {
		probe, probeErr := r.store.ScanActiveMemory(ctx, boyan.ActiveMemoryScanRequest{Scope: scope, AfterItemID: active[len(active)-1].Item.ItemID, Limit: 1})
		if probeErr != nil {
			return nil, probeErr
		}
		if len(probe) != 0 {
			return nil, boyan.PermanentError(boyan.CodeLimitExceeded, "canonical boundary view exceeds the bounded active limit", nil)
		}
	}
	result := make([]boyan.RecallRecord, 0, len(active))
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

var _ boyan.CanonicalBoundaryViewReader = (*CanonicalReader)(nil)

// LoadCanonicalCompatibility resolves one exact canonical revision's
// rebuildable legacy presentation metadata, including superseded revisions.
// It is used only for idempotent boundary-operation replay; active reads stay
// on LoadActiveRecallRecords and never widen their bounded view.
func (r *CanonicalReader) LoadCanonicalCompatibility(ctx context.Context, scope boyan.Scope, revisionID string) (boyan.CanonicalCompatibilityPayload, bool, error) {
	if err := canonicalReaderContext(ctx); err != nil {
		return boyan.CanonicalCompatibilityPayload{}, false, err
	}
	if err := scope.Validate(); err != nil {
		return boyan.CanonicalCompatibilityPayload{}, false, err
	}
	view, found, err := r.loadCanonicalView(ctx, scope, revisionID, false)
	if err != nil || !found {
		return boyan.CanonicalCompatibilityPayload{}, found, err
	}
	payload, err := r.store.LoadPayload(ctx, view.revision.Payload)
	if err != nil {
		return boyan.CanonicalCompatibilityPayload{}, false, err
	}
	compatibility, ok := canonicalCompatibilityPayload(payload, scope)
	return compatibility, ok, nil
}

// LoadCanonicalBoundaryView exposes only a validated bounded compatibility
// view. Canonical v2 IDs remain in LegacyToCanonical for write translation.
func (r *CanonicalReader) LoadCanonicalBoundaryView(ctx context.Context, scope boyan.Scope) (boyan.CanonicalBoundaryView, error) {
	records, err := r.LoadActiveRecallRecords(ctx, scope, boyan.MaxRecallCandidates)
	if err != nil {
		return boyan.CanonicalBoundaryView{}, err
	}
	state, err := r.store.LoadScopeState(ctx, scope)
	if err != nil {
		return boyan.CanonicalBoundaryView{}, err
	}
	view := boyan.CanonicalBoundaryView{Snapshot: boyan.ScopeSnapshot{SchemaVersion: boyan.DerivedSchemaVersionV1, Scope: scope, Version: state.Version}, LegacyToCanonical: make(map[boyan.RevisionRef]boyan.RevisionRef)}
	for _, record := range records {
		provenance := boyan.Provenance{ParentRevisions: append([]boyan.RevisionRef(nil), record.LegacyParents...)}
		if record.LegacyKind == nil || *record.LegacyKind == boyan.DerivedKindAtom {
			provenance.RawSources = append(provenance.RawSources, record.SourceRefs...)
		}
		if err := provenance.Validate(scope); err != nil {
			return boyan.CanonicalBoundaryView{}, err
		}
		kind := boyan.DerivedKindAtom
		if record.LegacyKind != nil {
			kind = *record.LegacyKind
		}
		operationStage := boyan.OperationStageAtoms
		if kind == boyan.DerivedKindScenario {
			operationStage = boyan.OperationStageScenarios
		}
		if kind == boyan.DerivedKindProfile {
			operationStage = boyan.OperationStageProfile
		}
		operationID := record.LegacyOperationID
		if operationID == "" {
			var opErr error
			operationID, opErr = boyan.ProcessingOperationID(operationStage, record.RevisionID, boyan.LegacyDerivationRef())
			if opErr != nil {
				return boyan.CanonicalBoundaryView{}, opErr
			}
		}
		legacyRef := boyan.RevisionRef{}
		switch kind {
		case boyan.DerivedKindAtom:
			category := boyan.AtomCategoryFact
			if record.Category != nil {
				category = *record.Category
			}
			itemID := record.LegacyItemID
			if itemID == "" {
				var idErr error
				itemID, idErr = boyan.AtomItemID(scope, category, record.Text)
				if idErr != nil {
					return boyan.CanonicalBoundaryView{}, idErr
				}
			}
			meta := boyan.RevisionMeta{SchemaVersion: boyan.DerivedSchemaVersionV1, Kind: kind, ItemID: itemID, Revision: record.Revision, OperationID: operationID, Scope: scope, State: boyan.RevisionStateActive, Provenance: provenance, CreatedAt: record.CreatedAt}
			revisionID := record.LegacyRevisionID
			if revisionID == "" {
				var idErr error
				revisionID, idErr = boyan.DerivedRevisionID(scope, itemID, operationID, []string{string(category), record.Text, string(boyan.CandidateRelationNew)}, provenance, nil)
				if idErr != nil {
					return boyan.CanonicalBoundaryView{}, idErr
				}
			}
			meta.RevisionID = revisionID
			atom := boyan.Atom{Meta: meta, Category: category, Text: record.Text, Relation: boyan.CandidateRelationNew}
			if err := atom.Validate(); err != nil {
				return boyan.CanonicalBoundaryView{}, err
			}
			view.Snapshot.Atoms = append(view.Snapshot.Atoms, atom)
			legacyRef = boyan.RevisionRef{ItemID: itemID, RevisionID: revisionID}
		case boyan.DerivedKindScenario:
			itemID := record.LegacyItemID
			if itemID == "" {
				var idErr error
				itemID, idErr = boyan.ScenarioItemID(scope, record.TopicKey)
				if idErr != nil {
					return boyan.CanonicalBoundaryView{}, idErr
				}
			}
			meta := boyan.RevisionMeta{SchemaVersion: boyan.DerivedSchemaVersionV1, Kind: kind, ItemID: itemID, Revision: record.Revision, OperationID: operationID, Scope: scope, State: boyan.RevisionStateActive, Provenance: provenance, CreatedAt: record.CreatedAt}
			revisionID := record.LegacyRevisionID
			if revisionID == "" {
				var idErr error
				revisionID, idErr = boyan.DerivedRevisionID(scope, itemID, operationID, []string{record.TopicKey, record.Title, record.Text}, provenance, nil)
				if idErr != nil {
					return boyan.CanonicalBoundaryView{}, idErr
				}
			}
			if record.LegacySupersedes != nil {
				copyOf := *record.LegacySupersedes
				meta.Supersedes = &copyOf
			}
			meta.RevisionID = revisionID
			scenario := boyan.Scenario{Meta: meta, TopicKey: record.TopicKey, Title: record.Title, Summary: record.Text}
			if err := scenario.Validate(); err != nil {
				return boyan.CanonicalBoundaryView{}, err
			}
			view.Snapshot.Scenarios = append(view.Snapshot.Scenarios, scenario)
			legacyRef = boyan.RevisionRef{ItemID: itemID, RevisionID: revisionID}
		case boyan.DerivedKindProfile:
			itemID := record.LegacyItemID
			if itemID == "" {
				var idErr error
				itemID, idErr = boyan.ProfileItemID(scope)
				if idErr != nil {
					return boyan.CanonicalBoundaryView{}, idErr
				}
			}
			meta := boyan.RevisionMeta{SchemaVersion: boyan.DerivedSchemaVersionV1, Kind: kind, ItemID: itemID, Revision: record.Revision, OperationID: operationID, Scope: scope, State: boyan.RevisionStateActive, Provenance: provenance, CreatedAt: record.CreatedAt}
			revisionID := record.LegacyRevisionID
			if revisionID == "" {
				var idErr error
				revisionID, idErr = boyan.DerivedRevisionID(scope, itemID, operationID, []string{record.Text}, provenance, nil)
				if idErr != nil {
					return boyan.CanonicalBoundaryView{}, idErr
				}
			}
			if record.LegacySupersedes != nil {
				copyOf := *record.LegacySupersedes
				meta.Supersedes = &copyOf
			}
			meta.RevisionID = revisionID
			profile := boyan.Profile{Meta: meta, Summary: record.Text}
			if err := profile.Validate(); err != nil {
				return boyan.CanonicalBoundaryView{}, err
			}
			view.Snapshot.Profiles = append(view.Snapshot.Profiles, profile)
			legacyRef = boyan.RevisionRef{ItemID: itemID, RevisionID: revisionID}
		}
		view.LegacyToCanonical[legacyRef] = boyan.RevisionRef{ItemID: record.ItemID, RevisionID: record.RevisionID}
	}
	if err := view.Validate(); err != nil {
		return boyan.CanonicalBoundaryView{}, err
	}
	return view, nil
}
