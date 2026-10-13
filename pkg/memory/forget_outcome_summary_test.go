package memory_test

import (
	"fmt"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestForgetOutcomeSummaryValidation(t *testing.T) {
	scope := memory.Scope{Key: "test:summary", Kind: memory.ScopeKindPersonal}
	base := memory.ForgetOutcome{
		SchemaVersion: memory.DerivedSchemaVersionV1, OperationID: "forget", Kind: memory.ForgetKindScope,
		Scope: scope, ScopeVersion: 1,
	}

	tests := []struct {
		name               string
		sources, revisions uint64
		truncated          bool
		refs               int
		valid              bool
	}{
		{name: "legacy lists", refs: 1, valid: true},
		{name: "empty", valid: true},
		{name: "exact cap", revisions: 2048, refs: 2048, valid: true},
		{name: "above cap summary", sources: 1025, revisions: 1025, truncated: true, valid: true},
		{name: "partial arrays", sources: 1025, revisions: 1025, truncated: true, refs: 1},
		{name: "missing truncation", sources: 1025, revisions: 1025},
		{name: "small false truncation", sources: 1, revisions: 1, truncated: true},
		{name: "wrong counts", revisions: 2, refs: 1},
		{name: "above cap lists", revisions: 2049, refs: 2049},
		{name: "overflow-safe summary", sources: ^uint64(0), revisions: 1, truncated: true, valid: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			outcome := base
			outcome.SourceCount = test.sources
			outcome.RevisionCount = test.revisions

			outcome.ReferencesTruncated = test.truncated
			for index := range test.refs {
				outcome.Revisions = append(outcome.Revisions, memory.RevisionRef{
					ItemID: "item", RevisionID: fmt.Sprintf("revision-%04d", index),
				})
			}

			if err := outcome.Validate(); (err == nil) != test.valid {
				t.Fatalf("Validate()=%v valid=%t", err, test.valid)
			}
		})
	}
}

func TestSourceForgetOutcomeRetainsSingleSourceContract(t *testing.T) {
	scope := memory.Scope{Key: "test:source-summary", Kind: memory.ScopeKindPersonal}
	source := memory.SourceRef{Scope: scope, ExportID: "export", ConversationID: "conversation", SourceTurnID: "turn"}
	base := memory.ForgetOutcome{
		SchemaVersion: memory.DerivedSchemaVersionV1, OperationID: "forget", Kind: memory.ForgetKindSource,
		Scope: scope, ScopeVersion: 1, Sources: []memory.SourceRef{source},
	}

	tests := []struct {
		name        string
		sourceCount uint64
		truncated   bool
		omitSource  bool
		valid       bool
	}{
		{name: "legacy source", valid: true},
		{name: "counted source", sourceCount: 1, valid: true},
		{name: "missing source", omitSource: true},
		{name: "inconsistent source count", sourceCount: 2},
		{name: "truncated source forbidden", sourceCount: 2049, truncated: true, omitSource: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			outcome := base
			outcome.SourceCount = test.sourceCount

			outcome.ReferencesTruncated = test.truncated
			if test.omitSource {
				outcome.Sources = nil
			}

			if err := outcome.Validate(); (err == nil) != test.valid {
				t.Fatalf("Validate()=%v valid=%t", err, test.valid)
			}
		})
	}
}
