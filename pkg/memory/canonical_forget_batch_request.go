package memory

import "time"

const maxCanonicalForgetBatch = 512

// CanonicalSourceForgetBatchRequest resumes one source's direct provenance
// traversal after AfterRevisionID. An empty cursor starts a new traversal.
type CanonicalSourceForgetBatchRequest struct {
	Scope           Scope     `json:"scope"`
	SourceID        string    `json:"source_id"`
	AfterRevisionID string    `json:"after_revision_id,omitempty"`
	Limit           uint32    `json:"limit"`
	DeniedAt        time.Time `json:"denied_at"`
}

// Validate verifies that a bounded source-forget batch cannot cross scopes or
// use an unbounded cursor request.
func (r CanonicalSourceForgetBatchRequest) Validate() error {
	if err := r.Scope.Validate(); err != nil {
		return err
	}

	if !isCanonicalID(r.SourceID) ||
		(r.AfterRevisionID != "" && !isCanonicalID(r.AfterRevisionID)) ||
		r.Limit == 0 ||
		r.Limit > maxCanonicalForgetBatch ||
		r.DeniedAt.IsZero() {
		return invalidDerived("canonical source forget batch request is invalid")
	}

	return nil
}

// CanonicalSourceForgetBatchResult reports only immutable identities so it is
// safe to persist as a checkpoint without retaining removed payload content.
type CanonicalSourceForgetBatchResult struct {
	RevisionIDs []string `json:"revision_ids,omitempty"`
	NextCursor  string   `json:"next_cursor,omitempty"`
}
