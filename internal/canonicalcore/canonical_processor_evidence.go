package canonicalcore

import . "github.com/baldaworks/boyan/pkg/memory"

func canonicalizeCandidate(
	sourceID string,
	messages map[string]Message,
	candidate SemanticCandidate,
) (
	SemanticCandidate,
	error,
) {
	if err := candidate.Memory.Validate(); err != nil {
		return SemanticCandidate{}, err
	}

	if len(candidate.Memory.Evidence) == 0 {
		return SemanticCandidate{}, invalidDerived("canonical candidate evidence is required")
	}

	canonical := candidate

	canonical.Memory.Evidence = make([]EvidenceRef, 0, len(candidate.Memory.Evidence))
	for _, claimed := range candidate.Memory.Evidence {
		message, ok := messages[claimed.MessageID]
		if !ok || claimed.SourceID != sourceID || claimed.Role != message.Role {
			return SemanticCandidate{}, PermanentError(
				CodeScopeViolation,
				"canonical evidence is not grounded in the captured turn",
				nil,
			)
		}

		ref, err := NewEvidenceRef(
			sourceID,
			message.MessageID,
			message.Role,
			message.Text,
			claimed.StartByte,
			claimed.EndByte,
			claimed.AssertionMode,
		)
		if err != nil {
			return SemanticCandidate{}, err
		}

		if claimed.TextDigest != "" && claimed.TextDigest != ref.TextDigest {
			return SemanticCandidate{}, invalidDerived("canonical evidence text digest does not match captured message")
		}

		canonical.Memory.Evidence = append(canonical.Memory.Evidence, ref)
	}

	return canonical, nil
}

func validateActiveCanonicalMemory(scope Scope, active ActiveCanonicalMemory) error {
	if err := active.Item.Validate(); err != nil {
		return PermanentError(CodeStoreFailure, "stored active canonical item is invalid", err)
	}

	if active.Item.Scope != scope || !isCanonicalID(active.RevisionID) ||
		active.Revision == 0 {
		return PermanentError(CodeStoreFailure, "stored active canonical revision is invalid", nil)
	}

	if err := validateEvidenceRefs(active.Evidence); err != nil {
		return PermanentError(CodeStoreFailure, "stored active canonical evidence is invalid", err)
	}

	return nil
}

func activeRevision(active []ActiveCanonicalMemory, item *MemoryItem) (*MemoryRevision, uint64, error) {
	if item == nil {
		return nil, 1, nil
	}

	for _, existing := range active {
		if existing.Item.ItemID == item.ItemID {
			return &MemoryRevision{RevisionID: existing.RevisionID, Revision: existing.Revision}, existing.Revision + 1, nil
		}
	}

	return nil, 0, PermanentError(CodeStoreFailure, "reconciler selected an unobserved active item", nil)
}
