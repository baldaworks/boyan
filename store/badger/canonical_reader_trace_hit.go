package badger

import memory "github.com/baldaworks/boyan/pkg/memory"

func categoryForCanonicalView(view canonicalRecallView) memory.AtomCategory {
	if view.item.Kind == memory.MemoryKindEvent {
		return memory.AtomCategoryEvent
	}

	return memory.AtomCategoryFact
}

func canonicalTraceHit(
	scope memory.Scope,
	view canonicalRecallView,
	text []byte,
	provenance memory.Provenance,
	category memory.AtomCategory,
) (memory.SearchHit, error) {
	if view.compat == nil {
		meta := memory.RevisionMeta{
			SchemaVersion: memory.DerivedSchemaVersionV1,
			Kind:          memory.DerivedKindAtom,
			ItemID:        view.item.ItemID,
			RevisionID:    view.revision.RevisionID,
			Revision:      view.revision.Revision,
			OperationID:   view.revision.RevisionID,
			Scope:         scope,
			State:         view.state,
			Provenance:    provenance,
			CreatedAt:     view.revision.Temporal.ObservedAt,
		}
		atom := memory.Atom{
			Meta:     meta,
			Category: category,
			Text:     string(text),
			Relation: memory.CandidateRelationNew,
		}

		hit := memory.SearchHit{Atom: &atom}

		return validatedCanonicalTraceHit(hit)
	}

	return canonicalCompatibilityTraceHit(scope, view, provenance)
}

func canonicalCompatibilityTraceHit(
	scope memory.Scope,
	view canonicalRecallView,
	provenance memory.Provenance,
) (memory.SearchHit, error) {
	compat := *view.compat
	legacyProvenance := provenance

	legacyProvenance.ParentRevisions = append([]memory.RevisionRef(nil), compat.LegacyParents...)
	if compat.Kind != memory.DerivedKindAtom {
		legacyProvenance.RawSources = nil
	}

	meta := memory.RevisionMeta{
		SchemaVersion: memory.DerivedSchemaVersionV1,
		Kind:          compat.Kind,
		ItemID:        compat.LegacyItemID,
		RevisionID:    compat.LegacyRevisionID,
		Revision:      view.revision.Revision,
		OperationID:   compat.LegacyOperationID,
		Scope:         scope,
		State:         view.state,
		Provenance:    legacyProvenance,
		CreatedAt:     view.revision.Temporal.ObservedAt,
	}
	if compat.Supersedes != nil {
		copyOf := *compat.Supersedes
		meta.Supersedes = &copyOf
	}

	if meta.OperationID == "" {
		return memory.SearchHit{}, memory.PermanentError(
			memory.CodeInvalidDerived,
			"canonical compatibility operation identity is unavailable",
			nil,
		)
	}

	return canonicalCompatibilityHit(meta, compat)
}

func canonicalCompatibilityHit(
	meta memory.RevisionMeta,
	compat memory.CanonicalCompatibilityPayload,
) (memory.SearchHit, error) {
	switch compat.Kind {
	case memory.DerivedKindScenario:
		hit := memory.SearchHit{
			Scenario: &memory.Scenario{Meta: meta, TopicKey: compat.TopicKey, Title: compat.Title, Summary: compat.Text},
		}

		return validatedCanonicalTraceHit(hit)
	case memory.DerivedKindProfile:
		hit := memory.SearchHit{Profile: &memory.Profile{Meta: meta, Summary: compat.Text}}

		return validatedCanonicalTraceHit(hit)
	case memory.DerivedKindAtom:
		hit := memory.SearchHit{
			Atom: &memory.Atom{
				Meta:     meta,
				Category: *compat.Category,
				Text:     compat.Text,
				Relation: memory.CandidateRelationNew,
			},
		}

		return validatedCanonicalTraceHit(hit)
	default:
		return memory.SearchHit{}, memory.PermanentError(
			memory.CodeInvalidDerived,
			"canonical compatibility kind is invalid",
			nil,
		)
	}
}

func validatedCanonicalTraceHit(hit memory.SearchHit) (memory.SearchHit, error) {
	if _, err := hit.Validate(); err != nil {
		return memory.SearchHit{}, err
	}

	return hit, nil
}
