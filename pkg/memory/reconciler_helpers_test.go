package memory

import "time"

func reconciliationCandidate(kind MemoryKind, subject, predicate string, qualifiers []string) SemanticCandidate {
	return SemanticCandidate{
		Kind: kind, Subject: subject, Predicate: predicate, Qualifiers: qualifiers,
		Memory: MemoryCandidate{
			Kind: kind, Statement: "candidate", Temporal: Temporal{ObservedAt: time.Date(2026, time.August, 5, 0, 0, 0, 0, time.UTC)},
			Evidence:    []EvidenceRef{{SourceID: "source-1", MessageID: "message-1", Role: MessageRoleUser, StartByte: 1, EndByte: 2, AssertionMode: AssertionModeUser}},
			Sensitivity: SensitivityStandard, Retention: RetentionClassStandard,
		},
	}
}
