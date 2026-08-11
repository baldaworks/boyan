package badger

import memory "github.com/baldaworks/boyan/pkg/memory"

func canonicalBoundaryProvenance(scope memory.Scope, record memory.RecallRecord) (memory.Provenance, error) {
	provenance := memory.Provenance{ParentRevisions: append([]memory.RevisionRef(nil), record.LegacyParents...)}
	if record.LegacyKind == nil || *record.LegacyKind == memory.DerivedKindAtom {
		provenance.RawSources = append(provenance.RawSources, record.SourceRefs...)
	}
	if err := provenance.Validate(scope); err != nil {
		return memory.Provenance{}, err
	}
	return provenance, nil
}

func canonicalBoundaryKind(record memory.RecallRecord) memory.DerivedKind {
	if record.LegacyKind == nil {
		return memory.DerivedKindAtom
	}
	return *record.LegacyKind
}

func canonicalBoundaryOperationID(scope memory.Scope, record memory.RecallRecord, kind memory.DerivedKind) (string, error) {
	if record.LegacyOperationID != "" {
		return record.LegacyOperationID, nil
	}
	stage := memory.OperationStageAtoms
	if kind == memory.DerivedKindScenario {
		stage = memory.OperationStageScenarios
	}
	if kind == memory.DerivedKindProfile {
		stage = memory.OperationStageProfile
	}
	return memory.ProcessingOperationID(stage, record.RevisionID, memory.LegacyDerivationRef())
}

func cloneBoundarySupersedes(ref *memory.RevisionRef) *memory.RevisionRef {
	if ref == nil {
		return nil
	}
	copyOf := *ref
	return &copyOf
}
