package memory

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
