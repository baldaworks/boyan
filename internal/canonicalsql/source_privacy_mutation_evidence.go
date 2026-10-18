package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// A direct parent's evidence is bounded by the canonical revision contract.
// Read each parent once, never traverse its ancestry or a reverse index.
func mutationPrivacyEvidence(ctx context.Context, txn *Tx, scope memory.Scope, revision memory.MemoryRevision) (
	[]memory.EvidenceRef, bool, error,
) {
	if len(revision.Parents) > memory.MaxSourcesPerRevision {
		return nil, false, privacyEvidenceLimit()
	}

	complete, err := validatePrivacyEvidence(ctx, txn, scope, revision)
	if err != nil {
		return nil, false, err
	}

	refs := make(map[memory.EvidenceRef]struct{}, len(revision.Evidence))
	for _, ref := range revision.Evidence {
		refs[ref] = struct{}{}
	}

	for _, parentID := range revision.Parents {
		parent, valid, err := loadPrivacyParent(ctx, txn, scope, parentID)
		if err != nil {
			return nil, false, err
		}

		complete = complete && valid

		for _, ref := range parent.Evidence {
			refs[ref] = struct{}{}
			if len(refs) > memory.MaxSourcesPerRevision {
				return nil, false, privacyEvidenceLimit()
			}
		}
	}

	result := make([]memory.EvidenceRef, 0, len(refs))
	for ref := range refs {
		result = append(result, ref)
	}

	return result, complete, nil
}

func loadPrivacyParent(ctx context.Context, txn *Tx, scope memory.Scope,
	parentID string) (memory.MemoryRevision, bool, error) {
	key, err := privacyKey(scope, Revision, parentID)
	if err != nil {
		return memory.MemoryRevision{}, false, err
	}

	var parent memory.MemoryRevision
	if err := getPrivacyRecord(ctx, txn, key, Revision, &parent); err != nil {
		return parent, false, err
	}

	if parent.RevisionID != "" && parent.RevisionID != parentID {
		return parent, false, invalidSourcePrivacy()
	}

	if parent.Validate() != nil {
		return memory.MemoryRevision{}, false, nil
	}

	valid, err := validatePrivacyEvidence(ctx, txn, scope, parent)

	return parent, valid, err
}

func privacyEvidenceLimit() error {
	return memory.PermanentError(memory.CodeLimitExceeded, "canonical source privacy evidence exceeds limit", nil)
}
