package memory

// ReconciliationAction describes the application-owned mutation selected for a candidate.
type ReconciliationAction string

const (
	ReconciliationActionCreate    ReconciliationAction = "create"
	ReconciliationActionSupersede ReconciliationAction = "supersede"
)

// SemanticCandidate is untrusted semantic output. It intentionally carries no
// persistent key, item ID, scope, revision, or lifecycle state.
type SemanticCandidate struct {
	Kind       MemoryKind
	Subject    string
	Predicate  string
	Qualifiers []string
	Memory     MemoryCandidate
}

// PolicyRegistry canonicalizes the finite semantic vocabulary accepted by the
// portable reconciler. It is constructed by the application, not the model.
type PolicyRegistry struct {
	Version string
}

// Reconciliation is the deterministic, application-owned result of one candidate.
type Reconciliation struct {
	Action     ReconciliationAction
	Item       MemoryItem
	Supersedes *MemoryItem
	RevisionID string
	Provenance []EvidenceRef
	Lifecycle  LifecycleEvent
}

// ActiveMemoryItem is the bounded canonical context supplied by the store.
// Evidence is used only to preserve identity through an edit of one event.
type ActiveMemoryItem struct {
	Item     MemoryItem
	Evidence []EvidenceRef
}
