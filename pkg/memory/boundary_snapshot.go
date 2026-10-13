package memory

import "time"

// BoundaryParentMetadata binds a compatibility reference to canonical temporal
// and retention metadata. It is application-owned and never candidate output.
type BoundaryParentMetadata struct {
	CanonicalRef RevisionRef
	Temporal     Temporal
	Sensitivity  Sensitivity
	Retention    RetentionClass
}

// BoundarySnapshot separates current structural heads from eligible facts.
// Parents is nil only for legacy typed stores without canonical metadata.
type BoundarySnapshot struct {
	Structural ScopeSnapshot
	Eligible   ScopeView
	Parents    map[RevisionRef]BoundaryParentMetadata
}

// Validate checks bounded exact-scope membership and temporal metadata at T.
func (s BoundarySnapshot) Validate(maxItems int, at time.Time) error {
	if err := s.Structural.Validate(maxItems); err != nil {
		return err
	}

	if err := s.Eligible.Validate(maxItems); err != nil {
		return err
	}

	if s.Structural.Scope != s.Eligible.Scope || s.Structural.Version != s.Eligible.Version {
		return PermanentError(CodeScopeViolation, "boundary snapshot views disagree", nil)
	}

	if err := s.validateEligibleStates(); err != nil {
		return err
	}

	structural := boundarySnapshotRefs(s.Structural.Atoms, s.Structural.Scenarios, s.Structural.Profiles)

	eligible := boundarySnapshotRefs(s.Eligible.Atoms, s.Eligible.Scenarios, s.Eligible.Profiles)
	for ref := range eligible {
		if _, found := structural[ref]; !found {
			return invalidDerived("eligible boundary reference is not a structural head")
		}
	}

	if s.Parents == nil {
		return nil
	}

	return s.validateParentMetadata(structural, eligible, at)
}

func (s BoundarySnapshot) validateEligibleStates() error {
	for _, atom := range s.Eligible.Atoms {
		if atom.Meta.State != RevisionStateActive {
			return invalidDerived("boundary grounding atom is not active")
		}
	}

	for _, scenario := range s.Eligible.Scenarios {
		if scenario.Meta.State != RevisionStateActive {
			return invalidDerived("boundary grounding scenario is not active")
		}
	}

	for _, profile := range s.Eligible.Profiles {
		if profile.Meta.State != RevisionStateActive {
			return invalidDerived("boundary grounding profile is not active")
		}
	}

	return nil
}

func (s BoundarySnapshot) validateParentMetadata(structural, eligible map[RevisionRef]struct{}, at time.Time) error {
	if len(s.Parents) != len(structural) {
		return invalidDerived("canonical boundary metadata is incomplete")
	}

	for ref, parent := range s.Parents {
		if _, found := structural[ref]; !found {
			return invalidDerived("canonical boundary metadata reference is not a head")
		}

		if err := parent.CanonicalRef.Validate(); err != nil {
			return err
		}

		if err := parent.Temporal.Validate(); err != nil {
			return err
		}

		if err := validateSensitivityRetention(parent.Sensitivity, parent.Retention); err != nil {
			return err
		}

		if _, included := eligible[ref]; included &&
			(!TemporalEligibleAt(parent.Temporal, at) || parent.Sensitivity != SensitivityStandard) {
			return invalidDerived("boundary grounding parent is temporally ineligible")
		}
	}

	return nil
}

func boundarySnapshotRefs(atoms []Atom, scenarios []Scenario, profiles []Profile) map[RevisionRef]struct{} {
	refs := make(map[RevisionRef]struct{}, len(atoms)+len(scenarios)+len(profiles))
	for _, atom := range atoms {
		refs[RevisionRef{ItemID: atom.Meta.ItemID, RevisionID: atom.Meta.RevisionID}] = struct{}{}
	}

	for _, scenario := range scenarios {
		refs[RevisionRef{ItemID: scenario.Meta.ItemID, RevisionID: scenario.Meta.RevisionID}] = struct{}{}
	}

	for _, profile := range profiles {
		refs[RevisionRef{ItemID: profile.Meta.ItemID, RevisionID: profile.Meta.RevisionID}] = struct{}{}
	}

	return refs
}
