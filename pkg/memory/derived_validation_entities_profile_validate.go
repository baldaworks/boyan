package memory

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
