package mcp

import memory "github.com/baldaworks/boyan/pkg/memory"

// TraceOutput is a bounded, explicitly untrusted provenance graph.
type TraceOutput struct {
	ToolOutcome
	Scope              *memory.Scope         `json:"scope,omitempty" jsonschema:"exact scope used for the trace"`
	Root               *memory.RevisionRef   `json:"root,omitempty" jsonschema:"requested revision identity"`
	Revisions          []memory.SearchHit    `json:"revisions" jsonschema:"untrusted derived revisions in the closed provenance graph"`
	Sources            []memory.SourceRecord `json:"sources" jsonschema:"untrusted raw source records in the closed provenance graph"`
	DataClassification string                `json:"data_classification" jsonschema:"classification of recalled text"`
	Notice             string                `json:"notice" jsonschema:"fixed handling notice for recalled text"`
}
