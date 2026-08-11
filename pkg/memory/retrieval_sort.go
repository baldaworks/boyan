package memory

func derivedReferenceLess(left, right DerivedReference) bool {
	switch {
	case left.Score != nil && right.Score == nil:
		return true
	case left.Score == nil && right.Score != nil:
		return false
	case left.Score != nil && right.Score != nil && *left.Score != *right.Score:
		return *left.Score > *right.Score
	case !left.CreatedAt.Equal(right.CreatedAt):
		return left.CreatedAt.After(right.CreatedAt)
	default:
		return left.RevisionID < right.RevisionID
	}
}
