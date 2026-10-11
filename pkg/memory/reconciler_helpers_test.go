package memory_test

import memory "github.com/baldaworks/boyan/pkg/memory"

import "time"

func reconciliationCandidate(
	kind memory.MemoryKind,
	subject, predicate string,
	qualifiers []string,
) memory.SemanticCandidate {
	return memory.SemanticCandidate{
		Kind: kind, Subject: subject, Predicate: predicate, Qualifiers: qualifiers,
		Memory: memory.MemoryCandidate{
			Kind: kind, Statement: "candidate", Temporal: memory.Temporal{
				ObservedAt: time.Date(
					2026,
					time.August,
					5,
					0,
					0,
					0,
					0,
					time.UTC,
				),
			},
			Evidence: []memory.EvidenceRef{
				{
					SourceID:      "source-1",
					MessageID:     "message-1",
					Role:          memory.MessageRoleUser,
					StartByte:     1,
					EndByte:       2,
					AssertionMode: memory.AssertionModeUser,
				},
			},
			Sensitivity: memory.SensitivityStandard, Retention: memory.RetentionClassStandard,
		},
	}
}
