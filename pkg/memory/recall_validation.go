package memory

import (
	"strings"
	"time"
)

// Validate verifies a bounded untrusted recall response for one request.
func (r RecallResponse) Validate(request RecallRequest) error {
	if r.SchemaVersion != RecallSchemaVersionV1 || r.Trust != ReferenceTrustUntrusted || r.Scope != request.Scope {
		return PermanentError(CodeScopeViolation, "recall response scope or trust marker is invalid", nil)
	}
	if len(r.Results) > request.Limit {
		return limitExceeded("recall response exceeds the requested limit")
	}
	seen := make(map[string]struct{}, len(r.Results))
	for _, result := range r.Results {
		if result.SchemaVersion != RecallSchemaVersionV1 || result.Trust != ReferenceTrustUntrusted || result.Scope != request.Scope || result.State != RevisionStateActive || result.Text == "" {
			return invalidDerived("recall response contains an invalid reference")
		}
		if _, exists := seen[result.RevisionID]; exists {
			return invalidDerived("recall response contains duplicate revisions")
		}
		seen[result.RevisionID] = struct{}{}
	}
	return nil
}

// NormalizeRecallRequest applies defaults and validates an exact-scope query.
func NormalizeRecallRequest(request RecallRequest) (RecallRequest, error) {
	request.Query = strings.TrimSpace(request.Query)
	if request.SchemaVersion == "" {
		request.SchemaVersion = RecallSchemaVersionV1
	}
	if request.Limit == 0 {
		request.Limit = DefaultSearchLimit
	}
	if err := request.Validate(); err != nil {
		return RecallRequest{}, err
	}
	return request, nil
}

// Validate verifies a bounded exact-scope canonical recall query.
func (r RecallRequest) Validate() error {
	if r.SchemaVersion != RecallSchemaVersionV1 {
		return PermanentError(CodeInvalidQuery, "unsupported recall schema version", nil)
	}
	if err := r.Scope.Validate(); err != nil {
		return err
	}
	if r.Query == "" || strings.TrimSpace(r.Query) != r.Query || len(r.Query) > MaxSearchQueryBytes {
		return PermanentError(CodeInvalidQuery, "recall query is invalid", nil)
	}
	if r.Limit < 1 || r.Limit > MaxSearchLimit {
		return PermanentError(CodeInvalidQuery, "recall limit is outside the allowed range", nil)
	}
	if r.Kind != nil {
		switch *r.Kind {
		case MemoryKindState, MemoryKindEvent:
		default:
			return PermanentError(CodeInvalidQuery, "recall kind is invalid", nil)
		}
	}
	if r.Category != nil {
		if err := r.Category.Validate(); err != nil {
			return PermanentError(CodeInvalidQuery, "recall category is invalid", nil)
		}
	}
	for name, value := range map[string]string{"source": r.SourceID, "session": r.SessionID, "memory key": string(r.MemoryKey)} {
		if value != "" && !isCanonicalID(value) {
			return PermanentError(CodeInvalidQuery, name+" filter is invalid", nil)
		}
	}
	if r.AsOf != nil && r.AsOf.IsZero() {
		return PermanentError(CodeInvalidQuery, "recall as_of is invalid", nil)
	}
	return nil
}

// Validate verifies a projection document before it is indexed.
func (d RecallProjectionDocument) Validate() error {
	if err := d.Scope.Validate(); err != nil {
		return err
	}
	if !isCanonicalID(d.ItemID) || !isCanonicalID(d.RevisionID) || d.Revision == 0 || strings.TrimSpace(d.Text) == "" || len(d.Text) > MaxDerivedTextBytes || d.CreatedAt.IsZero() {
		return invalidDerived("recall projection document identity or text is invalid")
	}
	switch d.Kind {
	case MemoryKindState, MemoryKindEvent:
	default:
		return invalidDerived("recall projection document kind is invalid")
	}
	if d.Category != nil {
		if err := d.Category.Validate(); err != nil {
			return err
		}
	}
	if err := d.Temporal.Validate(); err != nil {
		return err
	}
	if err := validateSensitivityRetention(d.Sensitivity, d.Retention); err != nil {
		return err
	}
	for _, sourceID := range d.SourceIDs {
		if !isCanonicalID(sourceID) {
			return invalidDerived("recall projection source identity is invalid")
		}
	}
	for _, sessionID := range d.SessionIDs {
		if !isCanonicalID(sessionID) {
			return invalidDerived("recall projection session identity is invalid")
		}
	}
	if d.ScopeChangeSeq == 0 {
		return invalidDerived("recall projection change sequence is required")
	}
	return nil
}

// Validate verifies hydrated canonical recall data before it is exposed.
func (r RecallRecord) Validate(request RecallRequest, now time.Time) error {
	if r.Scope != request.Scope {
		return PermanentError(CodeScopeViolation, "recall record scope does not match request", nil)
	}
	if !isCanonicalID(r.ItemID) || !isCanonicalID(r.RevisionID) || r.Revision == 0 || r.State != RevisionStateActive {
		return PermanentError(CodeForgotten, "recall record is not active", nil)
	}
	if r.LegacyKind != nil {
		if err := r.LegacyKind.Validate(); err != nil {
			return err
		}
		if !isCanonicalID(r.LegacyItemID) || !isCanonicalID(r.LegacyRevisionID) || !isCanonicalID(r.LegacyOperationID) {
			return invalidDerived("recall compatibility identity is invalid")
		}
		if r.LegacySupersedes != nil {
			if err := r.LegacySupersedes.Validate(); err != nil {
				return err
			}
		}
		if len(r.LegacyParents) > MaxSourcesPerRevision {
			return limitExceeded("recall compatibility parent bound exceeded")
		}
		for _, parent := range r.LegacyParents {
			if err := parent.Validate(); err != nil {
				return err
			}
		}
	}
	switch r.Kind {
	case MemoryKindState, MemoryKindEvent:
	default:
		return invalidDerived("recall record kind is invalid")
	}
	if r.Category != nil {
		if err := r.Category.Validate(); err != nil {
			return err
		}
	}
	if request.Kind != nil && r.Kind != *request.Kind {
		return PermanentError(CodeNotFound, "recall record does not match kind filter", nil)
	}
	if request.Category != nil && (r.Category == nil || *r.Category != *request.Category) {
		return PermanentError(CodeNotFound, "recall record does not match category filter", nil)
	}
	if request.MemoryKey != "" && r.MemoryKey != request.MemoryKey {
		return PermanentError(CodeNotFound, "recall record does not match memory filter", nil)
	}
	if strings.TrimSpace(r.Text) == "" || len(r.Text) > MaxDerivedTextBytes || r.CreatedAt.IsZero() {
		return invalidDerived("recall record text or timestamp is invalid")
	}
	if err := r.Temporal.Validate(); err != nil {
		return err
	}
	if r.Sensitivity != SensitivityStandard {
		return PermanentError(CodeForgotten, "recall record sensitivity is not allowed", nil)
	}
	if r.Retention != RetentionClassStandard && r.Retention != RetentionClassEphemeral {
		return PermanentError(CodeForgotten, "recall record retention is not allowed", nil)
	}
	if r.ScopeChangeSeq == 0 || r.ScopeChangeSeq < request.MinScopeChangeSeq {
		return PermanentError(CodeConflict, "recall record is behind the requested scope consistency", nil)
	}
	for _, sourceID := range r.SourceIDs {
		if !isCanonicalID(sourceID) {
			return invalidDerived("recall source identity is invalid")
		}
	}
	for _, source := range r.SourceRefs {
		if err := source.Validate(); err != nil {
			return err
		}
		if source.Scope != r.Scope {
			return PermanentError(CodeScopeViolation, "recall source reference scope does not match", nil)
		}
	}
	for _, sessionID := range r.SessionIDs {
		if !isCanonicalID(sessionID) {
			return invalidDerived("recall session identity is invalid")
		}
	}
	if request.SourceID != "" && !containsString(r.SourceIDs, request.SourceID) {
		return PermanentError(CodeNotFound, "recall record does not match source filter", nil)
	}
	if request.SessionID != "" && !containsString(r.SessionIDs, request.SessionID) {
		return PermanentError(CodeNotFound, "recall record does not match session filter", nil)
	}
	if len(r.SourceIDs) == 0 || len(r.Evidence) == 0 {
		return invalidDerived("recall record provenance is required")
	}
	if len(r.Evidence) > MaxRecallEvidence {
		return limitExceeded("recall evidence exceeds the compact bound")
	}
	for _, evidence := range r.Evidence {
		if err := evidence.Validate(); err != nil {
			return err
		}
		if evidence.SourceID == "" || !containsString(r.SourceIDs, evidence.SourceID) {
			return PermanentError(CodeScopeViolation, "recall evidence is not grounded in the record", nil)
		}
	}
	if request.AsOf != nil {
		asOf := request.AsOf.UTC()
		if r.Temporal.ValidFrom != nil && asOf.Before(r.Temporal.ValidFrom.UTC()) {
			return PermanentError(CodeNotFound, "recall record is not valid at as_of", nil)
		}
		if r.Temporal.ValidUntil != nil && asOf.After(r.Temporal.ValidUntil.UTC()) {
			return PermanentError(CodeNotFound, "recall record expired at as_of", nil)
		}
		if r.Temporal.ExpiresAt != nil && !asOf.Before(r.Temporal.ExpiresAt.UTC()) {
			return PermanentError(CodeNotFound, "recall record expired at as_of", nil)
		}
	} else if r.Temporal.ExpiresAt != nil && now.After(r.Temporal.ExpiresAt.UTC()) {
		return PermanentError(CodeNotFound, "recall record has expired", nil)
	}
	return nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
