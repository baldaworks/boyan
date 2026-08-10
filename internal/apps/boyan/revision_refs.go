package app

import memory "github.com/baldaworks/boyan/pkg/memory"

func revisionRef(meta memory.RevisionMeta) memory.RevisionRef {
	return memory.RevisionRef{ItemID: meta.ItemID, RevisionID: meta.RevisionID}
}

func appendUniqueRevisionRef(refs []memory.RevisionRef, ref memory.RevisionRef) []memory.RevisionRef {
	for _, current := range refs {
		if current == ref {
			return refs
		}
	}
	return append(refs, ref)
}
