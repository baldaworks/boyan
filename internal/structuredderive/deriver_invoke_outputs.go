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

const atomsOutputSchema = ("{\"type\":\"object\",\"properties\":{\"output\":{\"type\":\"array\",\"ite" +
	"ms\":{\"type\":\"object\"}}},\"required\":[\"output\"],\"additionalPro" +
	"perties\":false}")
const canonicalSemanticsOutputSchema = ("{\"type\":\"object\",\"properties\":{\"output\":{\"type\":\"array\",\"ite" +
	"ms\":{\"type\":\"object\"}}},\"required\":[\"output\"],\"additionalPro" +
	"perties\":false}")
const scenariosOutputSchema = ("{\"type\":\"object\",\"properties\":{\"output\":{\"type\":\"array\",\"ite" +
	"ms\":{\"type\":\"object\"}}},\"required\":[\"output\"],\"additionalPro" +
	"perties\":false}")
const profileOutputSchema = ("{\"type\":\"object\",\"properties\":{\"output\":{\"type\":\"object\"}},\"" +
	"required\":[\"output\"],\"additionalProperties\":false}")
