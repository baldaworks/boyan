package memory

import "time"

// CanonicalForgetCommitRequest is the content-free durable outcome written
// after logical denial. The store enforces the exact operation fingerprint and
// scope-version CAS, so replay cannot silently change the requested target.
type CanonicalForgetCommitRequest struct {
	Scope                Scope
	OperationID          string
	Kind                 ForgetKind
	Fingerprint          string
	ExpectedScopeVersion uint64
	Sources              []SourceRef
	Revisions            []RevisionRef
	ForgottenAt          time.Time
}

// Validate checks one content-free canonical forget outcome commit request.
func (r CanonicalForgetCommitRequest) Validate() error {
	if err := r.Scope.Validate(); err != nil {
		return err
	}

	if err := r.Kind.Validate(); err != nil {
		return err
	}

	if !isCanonicalID(r.OperationID) || !isCanonicalID(r.Fingerprint) ||
		r.ExpectedScopeVersion == ^uint64(0) ||
		r.ForgottenAt.IsZero() {
		return invalidDerived("canonical forget commit identity or timestamp is invalid")
	}

	return r.validateReferences()
}

func (r CanonicalForgetCommitRequest) validateReferences() error {
	for _, source := range r.Sources {
		if err := source.Validate(); err != nil {
			return err
		}

		if source.Scope != r.Scope {
			return PermanentError(CodeScopeViolation, "canonical forget source scope does not match", nil)
		}
	}

	for _, revision := range r.Revisions {
		if err := revision.Validate(); err != nil {
			return err
		}
	}

	return nil
}
