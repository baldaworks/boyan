package boundarycore

import memory "github.com/baldaworks/boyan/pkg/memory"

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

func activeAtoms(atoms []memory.Atom) []memory.Atom {
	active := make([]memory.Atom, 0, len(atoms))
	for _, atom := range atoms {
		if atom.Meta.State == memory.RevisionStateActive {
			active = append(active, atom)
		}
	}
	return cloneAtoms(active)
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

func activeProfiles(profiles []memory.Profile) []memory.Profile {
	active := make([]memory.Profile, 0, len(profiles))
	for _, profile := range profiles {
		if profile.Meta.State == memory.RevisionStateActive {
			active = append(active, profile)
		}
	}
	return cloneProfiles(active)
}
