package boundarycore

import memory "github.com/baldaworks/boyan/pkg/memory"

func cloneOperationOutcome(outcome memory.OperationOutcome) memory.OperationOutcome {
	outcome.Revisions = append([]memory.RevisionRef(nil), outcome.Revisions...)

	return outcome
}

func cloneCommitRequest(request memory.CommitRequest) memory.CommitRequest {
	request.Sources = cloneSources(request.Sources)
	request.Atoms = cloneAtoms(request.Atoms)
	request.Scenarios = cloneScenarios(request.Scenarios)
	request.Profiles = cloneProfiles(request.Profiles)
	request.Transitions = append([]memory.RevisionTransition(nil), request.Transitions...)

	return request
}

func cloneScopeSnapshot(snapshot memory.ScopeSnapshot) memory.ScopeSnapshot {
	snapshot.Sources = cloneSources(snapshot.Sources)
	snapshot.Atoms = cloneAtoms(snapshot.Atoms)
	snapshot.Scenarios = cloneScenarios(snapshot.Scenarios)
	snapshot.Profiles = cloneProfiles(snapshot.Profiles)

	return snapshot
}

func cloneAtoms(atoms []memory.Atom) []memory.Atom {
	cloned := append([]memory.Atom(nil), atoms...)
	for index := range cloned {
		cloned[index].Meta = cloneRevisionMeta(cloned[index].Meta)
		if cloned[index].RelatedRevision != nil {
			ref := *cloned[index].RelatedRevision
			cloned[index].RelatedRevision = &ref
		}
	}

	return cloned
}

func cloneScenarios(scenarios []memory.Scenario) []memory.Scenario {
	cloned := append([]memory.Scenario(nil), scenarios...)
	for index := range cloned {
		cloned[index].Meta = cloneRevisionMeta(cloned[index].Meta)
	}

	return cloned
}

func cloneProfiles(profiles []memory.Profile) []memory.Profile {
	cloned := append([]memory.Profile(nil), profiles...)
	for index := range cloned {
		cloned[index].Meta = cloneRevisionMeta(cloned[index].Meta)
	}

	return cloned
}

func cloneRevisionMeta(meta memory.RevisionMeta) memory.RevisionMeta {
	meta.Provenance.RawSources = append([]memory.SourceRef(nil), meta.Provenance.RawSources...)

	meta.Provenance.ParentRevisions = append([]memory.RevisionRef(nil), meta.Provenance.ParentRevisions...)
	if meta.Supersedes != nil {
		ref := *meta.Supersedes
		meta.Supersedes = &ref
	}

	return meta
}

func cloneTurn(turn memory.Turn) memory.Turn {
	turn.Messages = append([]memory.Message(nil), turn.Messages...)

	return turn
}

func cloneSources(sources []memory.SourceRecord) []memory.SourceRecord {
	cloned := append([]memory.SourceRecord(nil), sources...)
	for index := range cloned {
		if cloned[index].Turn != nil {
			turn := cloneTurn(*cloned[index].Turn)
			cloned[index].Turn = &turn
		}

		if cloned[index].ForgottenAt != nil {
			forgottenAt := *cloned[index].ForgottenAt
			cloned[index].ForgottenAt = &forgottenAt
		}
	}

	return cloned
}
