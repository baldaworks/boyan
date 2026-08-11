package memory

// Validate verifies a bounded untrusted derived search response.
func (r DerivedSearchResponse) Validate(limit int) error {
	if r.SchemaVersion != DerivedSchemaVersionV1 || r.Trust != ReferenceTrustUntrusted {
		return invalidDerived("derived search response schema or trust marker is invalid")
	}
	if err := r.Scope.Validate(); err != nil {
		return err
	}
	if limit < 1 || len(r.Results) > limit {
		return limitExceeded("derived search response exceeds the result limit")
	}
	seen := make(map[RevisionRef]struct{}, len(r.Results))
	for _, result := range r.Results {
		if err := result.Validate(); err != nil {
			return err
		}
		if result.Scope != r.Scope {
			return PermanentError(CodeScopeViolation, "derived search result scope does not match the response", nil)
		}
		ref := RevisionRef{ItemID: result.ItemID, RevisionID: result.RevisionID}
		if _, ok := seen[ref]; ok {
			return invalidDerived("derived search response contains a duplicate revision")
		}
		seen[ref] = struct{}{}
	}
	for index := 1; index < len(r.Results); index++ {
		if derivedReferenceLess(r.Results[index], r.Results[index-1]) {
			return invalidDerived("derived search response ordering is not deterministic")
		}
	}
	return nil
}
