package memory

// Validate verifies a bounded untrusted recall response for one request.
func (r RecallResponse) Validate(request RecallRequest) error {
	if r.SchemaVersion != RecallSchemaVersionV1 || r.Trust != ReferenceTrustUntrusted ||
		r.Scope != request.Scope {
		return PermanentError(CodeScopeViolation, "recall response scope or trust marker is invalid", nil)
	}

	if len(r.Results) > request.Limit {
		return limitExceeded("recall response exceeds the requested limit")
	}

	seen := make(map[string]struct{}, len(r.Results))
	for _, result := range r.Results {
		if err := validateRecallReference(result, request.Scope); err != nil {
			return err
		}

		if _, exists := seen[result.RevisionID]; exists {
			return invalidDerived("recall response contains duplicate revisions")
		}

		seen[result.RevisionID] = struct{}{}
	}

	return nil
}

func validateRecallReference(result RecallReference, scope Scope) error {
	if result.SchemaVersion != RecallSchemaVersionV1 ||
		result.Trust != ReferenceTrustUntrusted ||
		result.Scope != scope ||
		result.State != RevisionStateActive ||
		result.Text == "" {
		return invalidDerived("recall response contains an invalid reference")
	}

	return nil
}
