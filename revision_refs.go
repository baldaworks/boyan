package boyan

func revisionRef(meta RevisionMeta) RevisionRef {
	return RevisionRef{ItemID: meta.ItemID, RevisionID: meta.RevisionID}
}

func appendUniqueRevisionRef(refs []RevisionRef, ref RevisionRef) []RevisionRef {
	for _, current := range refs {
		if current == ref {
			return refs
		}
	}
	return append(refs, ref)
}
