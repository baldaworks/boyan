package memory

// Validate verifies syntactically grounded atom candidate output.
func (c AtomCandidate) Validate() error {
	if err := c.Category.Validate(); err != nil {
		return err
	}

	if _, err := validateDerivedText("atom candidate text", c.Text); err != nil {
		return err
	}

	if err := c.Relation.Validate(); err != nil {
		return err
	}

	switch c.Relation {
	case CandidateRelationNew:
		if c.Target != nil {
			return invalidDerived("new atom candidate cannot have a target")
		}
	case CandidateRelationCoexist, CandidateRelationSupersede:
		if c.Target == nil {
			return invalidDerived("related atom candidate requires a target")
		}

		if err := c.Target.Validate(); err != nil {
			return err
		}
	}

	return nil
}
