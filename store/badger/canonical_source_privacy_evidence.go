package badger

import (
	"errors"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// A typed revision alone does not prove that its evidence belongs to the
// source. Require every message identity, role, bounds and full-text digest.
// Old incomplete compatibility records remain writable but uncertified.
func validatePrivacyEvidence(txn *badger.Txn, scope memory.Scope, revision memory.MemoryRevision) (bool, error) {
	if err := revision.Validate(); err != nil {
		return false, err
	}

	complete := true

	for _, ref := range revision.Evidence {
		valid, err := validatePrivacyEvidenceRef(txn, scope, ref)
		if err != nil {
			return false, err
		}

		complete = complete && valid
	}

	return complete, nil
}

func validatePrivacyEvidenceRef(txn *badger.Txn, scope memory.Scope, ref memory.EvidenceRef) (bool, error) {
	source, found, err := loadPrivacySource(txn, scope, ref.SourceID)
	if err != nil || !found {
		return false, err
	}

	key, err := canonicalKey(scope, badgerRecordMessage, ref.MessageID)
	if err != nil {
		return false, err
	}

	var message memory.MessageRecord

	err = getCanonicalRecord(txn, key, badgerRecordMessage, &message)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	valid, err := validatePrivacyMessage(message, ref)
	if err != nil || !valid {
		return false, err
	}

	return validatePrivacyRawEvidence(txn, source, ref)
}

func validatePrivacyMessage(message memory.MessageRecord, ref memory.EvidenceRef) (bool, error) {
	if message.Validate() != nil || message.MessageID != ref.MessageID {
		return false, invalidSourcePrivacy()
	}

	if message.SourceID != ref.SourceID || message.Role != ref.Role || ref.EndByte > message.Payload.ByteSize {
		return false, memory.PermanentError(memory.CodeInvalidDerived,
			"canonical privacy evidence ownership is invalid", nil)
	}

	return ref.TextDigest != "" && ref.TextDigest == message.Payload.Digest, nil
}

func mergePrivacyState(states map[string]string, sourceID, state string) {
	previous, exists := states[sourceID]
	if !exists || state == sourcePrivacySensitive || state == sourcePrivacyUnknown && previous != sourcePrivacySensitive {
		states[sourceID] = state
	}
}

// A direct parent's evidence is bounded by the canonical revision contract.
// Read each parent once, never traverse its ancestry or a reverse index.
func mutationPrivacyEvidence(txn *badger.Txn, scope memory.Scope, revision memory.MemoryRevision) (
	[]memory.EvidenceRef, bool, error,
) {
	if len(revision.Parents) > memory.MaxSourcesPerRevision {
		return nil, false, privacyEvidenceLimit()
	}

	complete, err := validatePrivacyEvidence(txn, scope, revision)
	if err != nil {
		return nil, false, err
	}

	refs := make(map[memory.EvidenceRef]struct{}, len(revision.Evidence))
	for _, ref := range revision.Evidence {
		refs[ref] = struct{}{}
	}

	for _, parentID := range revision.Parents {
		parent, valid, err := loadPrivacyParent(txn, scope, parentID)
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

func loadPrivacyParent(txn *badger.Txn, scope memory.Scope, parentID string) (memory.MemoryRevision, bool, error) {
	key, err := canonicalKey(scope, badgerRecordRevision, parentID)
	if err != nil {
		return memory.MemoryRevision{}, false, err
	}

	var parent memory.MemoryRevision
	if err := getCanonicalRecord(txn, key, badgerRecordRevision, &parent); err != nil {
		return parent, false, err
	}

	if parent.RevisionID != "" && parent.RevisionID != parentID {
		return parent, false, invalidSourcePrivacy()
	}

	if parent.Validate() != nil {
		return memory.MemoryRevision{}, false, nil
	}

	valid, err := validatePrivacyEvidence(txn, scope, parent)

	return parent, valid, err
}

func privacyEvidenceLimit() error {
	return memory.PermanentError(memory.CodeLimitExceeded, "canonical source privacy evidence exceeds limit", nil)
}
