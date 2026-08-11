package memory

// DerivedKind identifies one derived-memory layer.
type DerivedKind string

const (
	// DerivedKindAtom is an independently searchable statement.
	DerivedKindAtom DerivedKind = "atom"
	// DerivedKindScenario is a topic or project context summary.
	DerivedKindScenario DerivedKind = "scenario"
	// DerivedKindProfile is a long-lived exact-scope synthesis.
	DerivedKindProfile DerivedKind = "profile"
)

// AtomCategory classifies the meaning of an atom.
type AtomCategory string

const (
	// AtomCategoryFact is an asserted factual statement.
	AtomCategoryFact AtomCategory = "fact"
	// AtomCategoryPreference is a stated preference.
	AtomCategoryPreference AtomCategory = "preference"
	// AtomCategoryConstraint is a durable requirement or restriction.
	AtomCategoryConstraint AtomCategory = "constraint"
	// AtomCategoryDecision is a decision or commitment.
	AtomCategoryDecision AtomCategory = "decision"
	// AtomCategoryEvent is a time-relevant occurrence.
	AtomCategoryEvent AtomCategory = "event"
)

// CandidateRelation describes how an atom candidate relates to prior memory.
type CandidateRelation string

const (
	// CandidateRelationNew creates a new logical atom.
	CandidateRelationNew CandidateRelation = "new"
	// CandidateRelationCoexist preserves an explicit contradictory alternative.
	CandidateRelationCoexist CandidateRelation = "coexist"
	// CandidateRelationSupersede replaces one active atom revision.
	CandidateRelationSupersede CandidateRelation = "supersede"
)
