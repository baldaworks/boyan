package memory

// ActiveMemoryScanRequest pages active item metadata and the evidence of each
// active revision in deterministic item-ID order. It is the bounded context
// read used by the reconciler; implementations must not load full history.
type ActiveMemoryScanRequest struct {
	Scope       Scope
	AfterItemID string
	Limit       uint32
}

// Validate checks one bounded active-memory scan request.
func (r ActiveMemoryScanRequest) Validate() error {
	return ActiveHeadScanRequest(r).Validate()
}

// ActiveCanonicalMemory joins an active item to its current revision evidence.
// It intentionally excludes payload content and other historic revisions.
type ActiveCanonicalMemory struct {
	Item       MemoryItem
	RevisionID string
	Revision   uint64
	Evidence   []EvidenceRef
}
