package memory

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
