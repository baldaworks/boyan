//go:build integration

package standaloneruntime //nolint:testpackage // Shared extractor fixture exercises private runtime composition.

import (
	"context"

	m "github.com/baldaworks/boyan/pkg/memory"
)

type runtimeFixtureExtractor struct{}

func (runtimeFixtureExtractor) ExtractCanonicalSemantics(
	_ context.Context, request m.CanonicalExtractionRequest,
) ([]m.SemanticCandidate, error) {
	msg := request.Turn.Messages[0]

	evidence, err := m.NewEvidenceRef(request.Turn.SourceID, msg.MessageID, msg.Role, msg.Text, 0,
		uint32(len(msg.Text)), m.AssertionModeUser)
	if err != nil {
		return nil, err
	}

	return []m.SemanticCandidate{{Kind: m.MemoryKindState, Subject: msg.Text, Predicate: "memory",
		Memory: m.MemoryCandidate{Kind: m.MemoryKindState, Statement: msg.Text,
			Temporal: m.Temporal{ObservedAt: request.Turn.CompletedAt},
			Evidence: []m.EvidenceRef{evidence}, Sensitivity: m.SensitivityStandard, Retention: m.RetentionClassStandard}}}, nil
}
