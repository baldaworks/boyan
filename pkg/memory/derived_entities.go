package memory

// DerivationRef identifies the versioned policy used to derive one operation.
// Its values are stable, content-free fingerprints selected by the application,
// not model-provided identifiers.
type DerivationRef struct {
	Pipeline string `json:"pipeline"`
	Policy   string `json:"policy"`
	Prompt   string `json:"prompt"`
	Model    string `json:"model"`
}

// Atom is one independently searchable derived statement.
type Atom struct {
	Meta            RevisionMeta      `json:"meta"`
	Category        AtomCategory      `json:"category"`
	Text            string            `json:"text"`
	Relation        CandidateRelation `json:"relation"`
	RelatedRevision *RevisionRef      `json:"related_revision,omitempty"`
}

// Scenario is one topic or project context revision.
type Scenario struct {
	Meta     RevisionMeta `json:"meta"`
	TopicKey string       `json:"topic_key"`
	Title    string       `json:"title"`
	Summary  string       `json:"summary"`
}

// Profile is one long-lived exact-scope synthesis revision.
type Profile struct {
	Meta    RevisionMeta `json:"meta"`
	Summary string       `json:"summary"`
}
