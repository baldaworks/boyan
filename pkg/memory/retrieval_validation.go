package memory

import (
	"strings"
)

// NormalizeDerivedSearchRequest applies defaults and validates one exact-scope search.
func NormalizeDerivedSearchRequest(request DerivedSearchRequest) (DerivedSearchRequest, error) {
	request.Query = strings.TrimSpace(request.Query)
	if request.SchemaVersion == "" {
		request.SchemaVersion = DerivedSchemaVersionV1
	}
	if request.Limit == 0 {
		request.Limit = DefaultSearchLimit
	}
	if err := request.Validate(); err != nil {
		return DerivedSearchRequest{}, err
	}
	return request, nil
}

// Validate verifies one bounded exact-scope derived search request.
func (r DerivedSearchRequest) Validate() error {
	if r.SchemaVersion != DerivedSchemaVersionV1 {
		return PermanentError(CodeInvalidQuery, "unsupported derived search schema version", nil)
	}
	if err := r.Scope.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(r.Query) == "" || strings.TrimSpace(r.Query) != r.Query {
		return PermanentError(CodeInvalidQuery, "derived search query must be normalized", nil)
	}
	if len(r.Query) > MaxSearchQueryBytes {
		return PermanentError(CodeInvalidQuery, "derived search query exceeds the size limit", nil)
	}
	if r.Limit < 1 || r.Limit > MaxSearchLimit {
		return PermanentError(CodeInvalidQuery, "derived search limit is outside the allowed range", nil)
	}
	if r.Kind != nil {
		if err := r.Kind.Validate(); err != nil {
			return PermanentError(CodeInvalidQuery, "derived search kind is invalid", nil)
		}
	}
	if r.Category != nil {
		if err := r.Category.Validate(); err != nil {
			return PermanentError(CodeInvalidQuery, "derived search category is invalid", nil)
		}
		if r.Kind != nil && *r.Kind != DerivedKindAtom {
			return PermanentError(CodeInvalidQuery, "atom category filter requires atom kind", nil)
		}
	}
	for name, value := range map[string]string{"source": r.SourceID, "conversation": r.ConversationID, "memory key": r.MemoryKey} {
		if value != "" && !isCanonicalID(value) {
			return PermanentError(CodeInvalidQuery, name+" filter is invalid", nil)
		}
	}
	if r.AsOf != nil && r.AsOf.IsZero() {
		return PermanentError(CodeInvalidQuery, "derived search as_of is invalid", nil)
	}
	return nil
}
