package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// ScrubCanonicalForget removes only payload blobs already protected by the
// logical deny markers. Structural records, identities, and tombstone markers
// remain available for audit and replay-safe rejection.
func (s *CanonicalStore) ScrubCanonicalForget(ctx context.Context, scope memory.Scope, sourceIDs []string, revisionIDs []string) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}
	if err := scope.Validate(); err != nil {
		return err
	}
	if len(sourceIDs) > 4096 || len(revisionIDs) > 4096 {
		return memory.PermanentError(memory.CodeLimitExceeded, "canonical forget scrub bound exceeded", nil)
	}
	if s == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	deniedSources, err := normalizeDeniedIDs(sourceIDs, "source")
	if err != nil {
		return err
	}
	deniedRevisions, err := normalizeDeniedIDs(revisionIDs, "revision")
	if err != nil {
		return err
	}

	refs, scrubOverflow, err := s.collectForgetPayloadRefs(scope, deniedSources, deniedRevisions)
	if err != nil {
		return err
	}
	if scrubOverflow {
		return memory.PermanentError(memory.CodeLimitExceeded, "canonical forget scrub payload bound exceeded", nil)
	}
	for _, ref := range refs {
		if err := s.DeletePayload(ctx, ref); err != nil {
			return err
		}
	}
	return nil
}
