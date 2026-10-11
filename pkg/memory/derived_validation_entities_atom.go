package memory

func validateAtomRelation(atom Atom) error {
	switch atom.Relation {
	case CandidateRelationNew:
		if atom.RelatedRevision != nil || atom.Meta.Supersedes != nil {
			return invalidDerived("new atom cannot relate to a prior revision")
		}
	case CandidateRelationCoexist:
		if atom.RelatedRevision == nil {
			return invalidDerived("coexisting atom requires a related revision")
		}

		if atom.Meta.Supersedes != nil {
			return invalidDerived("coexisting atom cannot supersede a revision")
		}
	case CandidateRelationSupersede:
		if err := validateAtomSupersedes(atom); err != nil {
			return err
		}
	}

	return validateAtomRelatedProvenance(atom)
}

func validateAtomRelatedProvenance(atom Atom) error {
	if atom.RelatedRevision != nil {
		if err := atom.RelatedRevision.Validate(); err != nil {
			return err
		}

		if !containsRevisionRef(atom.Meta.Provenance.ParentRevisions, *atom.RelatedRevision) {
			return invalidDerived("related atom revision must be retained in provenance")
		}
	}

	return nil
}

func validateAtomSupersedes(atom Atom) error {
	if atom.RelatedRevision == nil || atom.Meta.Supersedes == nil {
		return invalidDerived("superseding atom requires a related revision")
	}

	if *atom.RelatedRevision != *atom.Meta.Supersedes {
		return invalidDerived("superseding atom relation does not match revision metadata")
	}

	return nil
}
