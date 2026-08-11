package badger

import memory "github.com/baldaworks/boyan/pkg/memory"

type canonicalBoundaryEntryView struct {
	atom      *memory.Atom
	scenario  *memory.Scenario
	profile   *memory.Profile
	legacyRef memory.RevisionRef
}

func canonicalBoundaryEntry(scope memory.Scope, record memory.RecallRecord) (canonicalBoundaryEntryView, error) {
	provenance, err := canonicalBoundaryProvenance(scope, record)
	if err != nil {
		return canonicalBoundaryEntryView{}, err
	}
	kind := canonicalBoundaryKind(record)
	operationID, err := canonicalBoundaryOperationID(scope, record, kind)
	if err != nil {
		return canonicalBoundaryEntryView{}, err
	}
	switch kind {
	case memory.DerivedKindScenario:
		return canonicalBoundaryScenario(scope, record, provenance, operationID)
	case memory.DerivedKindProfile:
		return canonicalBoundaryProfile(scope, record, provenance, operationID)
	default:
		return canonicalBoundaryAtom(scope, record, provenance, operationID)
	}
}
