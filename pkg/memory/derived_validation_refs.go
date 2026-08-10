package memory

// Validate verifies a complete, content-free derivation identity.
func (r DerivationRef) Validate() error {
	for _, field := range []string{r.Pipeline, r.Policy, r.Prompt, r.Model} {
		if !isCanonicalID(field) {
			return invalidDerived("derivation fingerprint is required")
		}
	}
	return nil
}

// Validate verifies a complete exact-scope raw source identity.
func (r SourceRef) Validate() error {
	if err := r.Scope.Validate(); err != nil {
		return err
	}
	if !isCanonicalID(r.ExportID) {
		return invalidDerived("source export id is required")
	}
	if !isCanonicalID(r.SessionID) {
		return invalidDerived("source session id is required")
	}
	if !isCanonicalID(r.SourceTurnID) {
		return invalidDerived("source turn id is required")
	}
	return nil
}

// Validate verifies an immutable derived revision identity.
func (r RevisionRef) Validate() error {
	if !isCanonicalID(r.ItemID) {
		return invalidDerived("derived item id is required")
	}
	if !isCanonicalID(r.RevisionID) {
		return invalidDerived("derived revision id is required")
	}
	return nil
}

// Validate verifies non-empty, bounded, duplicate-free same-scope provenance.
func (p Provenance) Validate(scope Scope) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	total := len(p.RawSources) + len(p.ParentRevisions)
	if total == 0 {
		return invalidDerived("derived provenance is required")
	}
	if total > MaxSourcesPerRevision {
		return limitExceeded("derived provenance exceeds the reference limit")
	}

	rawSeen := make(map[string]struct{}, len(p.RawSources))
	for _, source := range p.RawSources {
		if err := source.Validate(); err != nil {
			return err
		}
		if source.Scope != scope {
			return PermanentError(CodeScopeViolation, "raw source scope does not match the derived scope", nil)
		}
		key := source.Scope.Key + "\x00" + source.ExportID
		if _, ok := rawSeen[key]; ok {
			return invalidDerived("duplicate raw source reference")
		}
		rawSeen[key] = struct{}{}
	}

	parentSeen := make(map[RevisionRef]struct{}, len(p.ParentRevisions))
	for _, parent := range p.ParentRevisions {
		if err := parent.Validate(); err != nil {
			return err
		}
		if _, ok := parentSeen[parent]; ok {
			return invalidDerived("duplicate parent revision reference")
		}
		parentSeen[parent] = struct{}{}
	}
	return nil
}

func containsRevisionRef(refs []RevisionRef, want RevisionRef) bool {
	for _, ref := range refs {
		if ref == want {
			return true
		}
	}
	return false
}

func validateRevisionRefs(refs []RevisionRef) error {
	if len(refs) > MaxSourcesPerRevision {
		return limitExceeded("derived parent references exceed the limit")
	}
	seen := make(map[RevisionRef]struct{}, len(refs))
	for _, ref := range refs {
		if err := ref.Validate(); err != nil {
			return err
		}
		if _, ok := seen[ref]; ok {
			return invalidDerived("duplicate derived revision reference")
		}
		seen[ref] = struct{}{}
	}
	return nil
}
