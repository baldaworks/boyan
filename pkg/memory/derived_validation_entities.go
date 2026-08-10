package memory

// Validate verifies a complete atom revision and its deterministic identities.
func (a Atom) Validate() error {
	if err := a.Category.Validate(); err != nil {
		return err
	}
	text, err := validateDerivedText("atom text", a.Text)
	if err != nil {
		return err
	}
	if text != a.Text {
		return invalidDerived("atom text must be normalized")
	}
	if err := a.Relation.Validate(); err != nil {
		return err
	}
	if err := validateAtomRelation(a); err != nil {
		return err
	}
	itemID, err := AtomItemID(a.Meta.Scope, a.Category, a.Text)
	if err != nil {
		return err
	}
	contentParts := []string{string(a.Category), a.Text, string(a.Relation)}
	if a.RelatedRevision != nil {
		contentParts = append(contentParts, a.RelatedRevision.ItemID, a.RelatedRevision.RevisionID)
	}
	return validateRevisionMeta(a.Meta, DerivedKindAtom, itemID, contentParts, true)
}

// Validate verifies a complete scenario revision and its deterministic identities.
func (s Scenario) Validate() error {
	topicKey, err := validateDerivedKey("scenario topic key", s.TopicKey)
	if err != nil {
		return err
	}
	if topicKey != s.TopicKey {
		return invalidDerived("scenario topic key must be normalized")
	}
	title, err := validateDerivedText("scenario title", s.Title)
	if err != nil {
		return err
	}
	if title != s.Title {
		return invalidDerived("scenario title must be normalized")
	}
	summary, err := validateDerivedText("scenario summary", s.Summary)
	if err != nil {
		return err
	}
	if summary != s.Summary {
		return invalidDerived("scenario summary must be normalized")
	}
	itemID, err := ScenarioItemID(s.Meta.Scope, s.TopicKey)
	if err != nil {
		return err
	}
	return validateRevisionMeta(s.Meta, DerivedKindScenario, itemID, []string{s.TopicKey, s.Title, s.Summary}, false)
}

// Validate verifies a complete profile revision and its deterministic identities.
func (p Profile) Validate() error {
	summary, err := validateDerivedText("profile summary", p.Summary)
	if err != nil {
		return err
	}
	if summary != p.Summary {
		return invalidDerived("profile summary must be normalized")
	}
	itemID, err := ProfileItemID(p.Meta.Scope)
	if err != nil {
		return err
	}
	return validateRevisionMeta(p.Meta, DerivedKindProfile, itemID, []string{p.Summary}, false)
}

func validateRevisionMeta(
	meta RevisionMeta,
	kind DerivedKind,
	itemID string,
	contentParts []string,
	allowCrossItemSupersession bool,
) error {
	if meta.SchemaVersion != DerivedSchemaVersionV1 {
		return invalidDerived("unsupported derived revision schema version")
	}
	if meta.Kind != kind {
		return invalidDerived("derived revision kind does not match its value")
	}
	if meta.ItemID != itemID {
		return invalidDerived("derived item id does not match its stable identity")
	}
	if meta.Revision == 0 {
		return invalidDerived("derived revision number is required")
	}
	if !isCanonicalID(meta.OperationID) {
		return invalidDerived("derived operation id is required")
	}
	if err := meta.Scope.Validate(); err != nil {
		return err
	}
	if err := meta.State.Validate(); err != nil {
		return err
	}
	if err := meta.Provenance.Validate(meta.Scope); err != nil {
		return err
	}
	if meta.CreatedAt.IsZero() {
		return invalidDerived("derived creation time is required")
	}
	if meta.Supersedes != nil {
		if err := meta.Supersedes.Validate(); err != nil {
			return err
		}
		sameItem := meta.Supersedes.ItemID == meta.ItemID
		if sameItem && meta.Revision < 2 {
			return invalidDerived("superseding revision number must be greater than one")
		}
		if !sameItem && !allowCrossItemSupersession {
			return invalidDerived("superseded revision must belong to the same item")
		}
	}
	wantRevisionID, err := DerivedRevisionID(meta.Scope, meta.ItemID, meta.OperationID, contentParts, meta.Provenance, meta.Supersedes)
	if err != nil {
		return err
	}
	if meta.RevisionID != wantRevisionID {
		return invalidDerived("derived revision id does not match its stable identity")
	}
	return nil
}

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
		if atom.RelatedRevision == nil || atom.Meta.Supersedes == nil {
			return invalidDerived("superseding atom requires a related revision")
		}
		if *atom.RelatedRevision != *atom.Meta.Supersedes {
			return invalidDerived("superseding atom relation does not match revision metadata")
		}
	}
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
