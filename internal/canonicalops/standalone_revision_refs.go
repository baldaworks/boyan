package canonicalops

import memory "github.com/baldaworks/boyan/pkg/memory"

func revisionIDs(refs []memory.RevisionRef) []string {
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		ids = append(ids, ref.RevisionID)
	}
	return ids
}

func cloneOptionalRef(ref *memory.RevisionRef) *memory.RevisionRef {
	if ref == nil {
		return nil
	}
	copyOf := *ref
	return &copyOf
}
