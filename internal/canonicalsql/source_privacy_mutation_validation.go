package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func validatePrivacyEvidence(ctx context.Context, txn *Tx, scope memory.Scope,
	revision memory.MemoryRevision) (bool, error) {
	if err := revision.Validate(); err != nil {
		return false, err
	}

	complete := true

	for _, ref := range revision.Evidence {
		valid, err := validatePrivacyEvidenceRef(ctx, txn, scope, ref)
		if err != nil {
			return false, err
		}

		complete = complete && valid
	}

	return complete, nil
}

func mergePrivacyState(states map[string]string, sourceID, state string) {
	previous, exists := states[sourceID]
	if !exists || state == sourcePrivacySensitive || state == sourcePrivacyUnknown && previous != sourcePrivacySensitive {
		states[sourceID] = state
	}
}
