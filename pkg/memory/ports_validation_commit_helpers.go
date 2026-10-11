package memory

func addSupersessionRef(refs map[RevisionRef]struct{}, meta RevisionMeta) bool {
	if meta.Supersedes == nil {
		return true
	}

	if _, exists := refs[*meta.Supersedes]; exists {
		return false
	}

	refs[*meta.Supersedes] = struct{}{}

	return true
}

func validateSupersessionTransition(meta RevisionMeta, transitions []RevisionTransition) error {
	if meta.Supersedes == nil {
		return nil
	}

	transition, ok := transitionForRef(transitions, *meta.Supersedes)
	if !ok || transition.From != RevisionStateActive ||
		transition.To != RevisionStateSuperseded {
		return invalidDerived("superseding revision requires its active-to-superseded transition")
	}

	return nil
}

func transitionForRef(transitions []RevisionTransition, ref RevisionRef) (RevisionTransition, bool) {
	for _, transition := range transitions {
		if transition.Ref == ref {
			return transition, true
		}
	}

	return RevisionTransition{}, false
}

func validateCommitRevision(request CommitRequest, meta RevisionMeta, seen map[RevisionRef]struct{}) error {
	if meta.Scope != request.Scope {
		return PermanentError(CodeScopeViolation, "commit revision scope does not match the operation", nil)
	}

	if meta.OperationID != request.OperationID {
		return invalidDerived("commit revision operation does not match the request")
	}

	if meta.State != RevisionStateActive {
		return invalidDerived("new commit revision must be active")
	}

	ref := RevisionRef{ItemID: meta.ItemID, RevisionID: meta.RevisionID}
	if _, ok := seen[ref]; ok {
		return invalidDerived("commit contains duplicate derived revision")
	}

	seen[ref] = struct{}{}

	return nil
}
