package memory

// Validate verifies one bounded atomic Store commit.
func (r CommitRequest) Validate() error {
	if err := validateCommitRequestShape(r); err != nil {
		return err
	}

	if err := validateCommitSources(r); err != nil {
		return err
	}

	if err := validateCommitRevisions(r); err != nil {
		return err
	}

	return validateCommitTransitions(r)
}
