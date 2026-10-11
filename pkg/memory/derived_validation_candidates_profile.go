package memory

// Validate verifies syntactically grounded profile candidate output.
func (c ProfileCandidate) Validate() error {
	if err := c.Disposition.Validate(); err != nil {
		return err
	}

	if c.Disposition == ProfileDispositionSkip {
		if c.Summary != "" || len(c.Atoms) != 0 || len(c.Scenarios) != 0 || c.Supersedes != nil {
			return invalidDerived("skipped profile candidate cannot contain a revision")
		}

		return nil
	}

	return c.validateRevision()
}

func (c ProfileCandidate) validateRevision() error {
	if _, err := validateDerivedText("profile candidate summary", c.Summary); err != nil {
		return err
	}

	if len(c.Atoms)+len(c.Scenarios) == 0 {
		return invalidDerived("profile candidate requires a parent revision")
	}

	if len(c.Atoms)+len(c.Scenarios) > MaxSourcesPerRevision {
		return limitExceeded("profile candidate exceeds the parent reference limit")
	}

	if err := validateRevisionRefs(c.Atoms); err != nil {
		return err
	}

	if err := validateRevisionRefs(c.Scenarios); err != nil {
		return err
	}

	if c.Supersedes != nil {
		if err := c.Supersedes.Validate(); err != nil {
			return err
		}
	}

	return nil
}
