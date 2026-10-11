package boundarycore

import memory "github.com/baldaworks/boyan/pkg/memory"

func validateCommittedOutcome(request memory.CommitRequest, outcome memory.OperationOutcome) error {
	if err := outcome.Validate(); err != nil {
		return err
	}

	if outcome.OperationID != request.OperationID || outcome.Stage != request.Stage ||
		outcome.Scope != request.Scope {
		return memory.PermanentError(
			memory.CodeScopeViolation,
			"canonical commit outcome does not match the operation",
			nil,
		)
	}

	if outcome.ScopeVersion != request.ExpectedScopeVersion+1 {
		return memory.PermanentError(memory.CodeConflict, "canonical commit returned an unexpected scope version", nil)
	}

	if len(outcome.Revisions) != len(request.Atoms)+len(request.Scenarios)+len(request.Profiles) {
		return memory.PermanentError(
			memory.CodeInvalidDerived,
			"canonical commit outcome revision count does not match the request",
			nil,
		)
	}

	return validateCommittedRevisions(request, outcome)
}

func validateCommittedRevisions(request memory.CommitRequest, outcome memory.OperationOutcome) error {
	want := make(map[memory.RevisionRef]struct{}, len(outcome.Revisions))
	for _, atom := range request.Atoms {
		want[memory.RevisionRef{ItemID: atom.Meta.ItemID, RevisionID: atom.Meta.RevisionID}] = struct{}{}
	}

	for _, scenario := range request.Scenarios {
		want[memory.RevisionRef{ItemID: scenario.Meta.ItemID, RevisionID: scenario.Meta.RevisionID}] = struct{}{}
	}

	for _, profile := range request.Profiles {
		want[memory.RevisionRef{ItemID: profile.Meta.ItemID, RevisionID: profile.Meta.RevisionID}] = struct{}{}
	}

	for _, ref := range outcome.Revisions {
		if _, ok := want[ref]; !ok {
			return memory.PermanentError(
				memory.CodeInvalidDerived,
				"canonical commit outcome contains an unknown revision",
				nil,
			)
		}

		delete(want, ref)
	}

	return nil
}
