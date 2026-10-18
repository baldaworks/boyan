package canonicalsql

import memory "github.com/baldaworks/boyan/pkg/memory"

func validPrivacyMembership(ids []string) bool {
	if len(ids) > sourcePrivacyBatchSize {
		return false
	}

	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		ref := memory.PayloadRef{ID: id, Digest: "privacy", ByteSize: 1}
		if ref.Validate() != nil || seen[id] {
			return false
		}

		seen[id] = true
	}

	return true
}
