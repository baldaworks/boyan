package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// LoadCanonicalCompatibility resolves one exact canonical revision's
// rebuildable legacy presentation metadata, including superseded revisions.
// It is used only for idempotent boundary-operation replay; active reads stay
// on LoadActiveRecallRecords and never widen their bounded view.
func (r *CanonicalReader) LoadCanonicalCompatibility(
	ctx context.Context,
	scope memory.Scope,
	revisionID string,
) (memory.CanonicalCompatibilityPayload, bool, error) {
	if err := canonicalReaderContext(ctx); err != nil {
		return memory.CanonicalCompatibilityPayload{}, false, err
	}

	if err := scope.Validate(); err != nil {
		return memory.CanonicalCompatibilityPayload{}, false, err
	}

	view, found, err := r.loadCanonicalView(ctx, scope, revisionID, false)
	if err != nil || !found {
		return memory.CanonicalCompatibilityPayload{}, found, err
	}

	payload, err := r.store.LoadPayload(ctx, view.revision.Payload)
	if err != nil {
		return memory.CanonicalCompatibilityPayload{}, false, err
	}

	compatibility, ok := canonicalCompatibilityPayload(payload, scope)

	return compatibility, ok, nil
}
