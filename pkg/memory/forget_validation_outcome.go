package memory

// Validate verifies a content-free forgetting outcome.
func (o ForgetOutcome) Validate() error {
	if o.SchemaVersion != DerivedSchemaVersionV1 {
		return invalidDerived("unsupported forget outcome schema version")
	}

	if !isCanonicalID(o.OperationID) {
		return invalidDerived("forget outcome operation id is required")
	}

	if err := o.Kind.Validate(); err != nil {
		return err
	}

	if err := o.Scope.Validate(); err != nil {
		return err
	}

	if o.ScopeVersion == 0 {
		return invalidDerived("forget outcome scope version is required")
	}

	if len(o.Sources)+len(o.Revisions) > MaxForgetOutcomeReferences {
		return limitExceeded("forget outcome exceeds the item limit")
	}

	if o.Kind == ForgetKindSource && len(o.Sources) != 1 {
		return invalidDerived("source forget outcome requires exactly one source")
	}

	if err := o.validateSummary(); err != nil {
		return err
	}

	return o.validateSources()
}

func (o ForgetOutcome) validateSources() error {
	seen := make(map[string]struct{}, len(o.Sources))
	for _, source := range o.Sources {
		if err := source.Validate(); err != nil {
			return err
		}

		if source.Scope != o.Scope {
			return PermanentError(CodeScopeViolation, "forget outcome contains a foreign source", nil)
		}

		if _, ok := seen[source.ExportID]; ok {
			return invalidDerived("forget outcome contains a duplicate source")
		}

		seen[source.ExportID] = struct{}{}
	}

	return validateForgetRevisionRefs(o.Revisions, "forget outcome")
}
func validateForgetRevisionRefs(refs []RevisionRef, subject string) error {
	if len(refs) > MaxForgetOutcomeReferences {
		return limitExceeded(subject + " exceeds the revision limit")
	}

	seen := make(map[RevisionRef]struct{}, len(refs))
	for _, ref := range refs {
		if err := ref.Validate(); err != nil {
			return err
		}

		if _, ok := seen[ref]; ok {
			return invalidDerived(subject + " contains a duplicate revision")
		}

		seen[ref] = struct{}{}
	}

	return nil
}

func (o ForgetOutcome) validateSummary() error {
	if o.ReferencesTruncated {
		if o.Kind != ForgetKindScope || len(o.Sources)+len(o.Revisions) != 0 {
			return invalidDerived("truncated forget outcome must be a scope summary without reference arrays")
		}

		if !forgetCountsExceedLimit(o.SourceCount, o.RevisionCount) {
			return invalidDerived("truncated forget outcome counts must exceed the reference limit")
		}

		return nil
	}
	// Both zero fields preserve old outcomes whose exhaustive arrays carried the counts.
	if o.SourceCount == 0 && o.RevisionCount == 0 {
		return nil
	}

	if o.SourceCount != uint64(len(o.Sources)) || o.RevisionCount != uint64(len(o.Revisions)) {
		return invalidDerived("forget outcome counts do not match its exhaustive reference arrays")
	}

	return nil
}

func forgetCountsExceedLimit(sources, revisions uint64) bool {
	// Only add after each operand is bounded, so persisted uint64 counts cannot wrap.
	return sources > MaxForgetOutcomeReferences || revisions > MaxForgetOutcomeReferences ||
		sources+revisions > MaxForgetOutcomeReferences
}
