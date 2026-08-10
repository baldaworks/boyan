package app

import memory "github.com/baldaworks/boyan/pkg/memory"

func cloneTurn(turn memory.Turn) memory.Turn {
	turn.Messages = append([]memory.Message(nil), turn.Messages...)
	return turn
}

func scopeViewFromSnapshot(snapshot memory.ScopeSnapshot) memory.ScopeView {
	return memory.ScopeView{
		SchemaVersion: memory.DerivedSchemaVersionV1,
		Scope:         snapshot.Scope,
		Version:       snapshot.Version,
		Atoms:         activeAtoms(snapshot.Atoms),
		Scenarios:     activeScenarios(snapshot.Scenarios),
		Profiles:      activeProfiles(snapshot.Profiles),
	}
}

func cloneScopeSnapshot(snapshot memory.ScopeSnapshot) memory.ScopeSnapshot {
	snapshot.Sources = cloneSources(snapshot.Sources)
	snapshot.Atoms = cloneAtoms(snapshot.Atoms)
	snapshot.Scenarios = cloneScenarios(snapshot.Scenarios)
	snapshot.Profiles = cloneProfiles(snapshot.Profiles)
	return snapshot
}

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

func activeAtoms(atoms []memory.Atom) []memory.Atom {
	active := make([]memory.Atom, 0, len(atoms))
	for _, atom := range atoms {
		if atom.Meta.State == memory.RevisionStateActive {
			active = append(active, atom)
		}
	}
	return cloneAtoms(active)
}

func cloneScenarios(scenarios []memory.Scenario) []memory.Scenario {
	cloned := append([]memory.Scenario(nil), scenarios...)
	for index := range cloned {
		cloned[index].Meta = cloneRevisionMeta(cloned[index].Meta)
	}
	return cloned
}

func activeScenarios(scenarios []memory.Scenario) []memory.Scenario {
	active := make([]memory.Scenario, 0, len(scenarios))
	for _, scenario := range scenarios {
		if scenario.Meta.State == memory.RevisionStateActive {
			active = append(active, scenario)
		}
	}
	return cloneScenarios(active)
}

func cloneProfiles(profiles []memory.Profile) []memory.Profile {
	cloned := append([]memory.Profile(nil), profiles...)
	for index := range cloned {
		cloned[index].Meta = cloneRevisionMeta(cloned[index].Meta)
	}
	return cloned
}

func activeProfiles(profiles []memory.Profile) []memory.Profile {
	active := make([]memory.Profile, 0, len(profiles))
	for _, profile := range profiles {
		if profile.Meta.State == memory.RevisionStateActive {
			active = append(active, profile)
		}
	}
	return cloneProfiles(active)
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
