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
