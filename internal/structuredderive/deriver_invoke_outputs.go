package structuredderive

import memory "github.com/baldaworks/boyan/pkg/memory"

type atomsOutput struct {
	Output []memory.AtomCandidate `json:"output"`
}

type canonicalSemanticsOutput struct {
	Output []memory.SemanticCandidate `json:"output"`
}

type scenariosOutput struct {
	Output []memory.ScenarioCandidate `json:"output"`
}

type profileOutput struct {
	Output memory.ProfileCandidate `json:"output"`
}
