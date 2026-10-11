package memory

// Validate verifies syntactically grounded scenario candidate output.
func (c ScenarioCandidate) Validate() error {
	if _, err := validateDerivedKey("scenario candidate topic key", c.TopicKey); err != nil {
		return err
	}

	if _, err := validateDerivedText("scenario candidate title", c.Title); err != nil {
		return err
	}

	if _, err := validateDerivedText("scenario candidate summary", c.Summary); err != nil {
		return err
	}

	if len(c.Atoms) == 0 {
		return invalidDerived("scenario candidate requires an atom reference")
	}

	if err := validateRevisionRefs(c.Atoms); err != nil {
		return err
	}

	if c.Supersedes != nil {
		if err := c.Supersedes.Validate(); err != nil {
			return err
		}
	}

	return nil
}
