package structuredderive_test

import (
	"encoding/json"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestStructuredContractsPreserveMarshaledCandidates(t *testing.T) {
	t.Parallel()

	cases := marshaledCompatibilityCandidates()

	adapters := adapterContracts(t)
	for _, tc := range cases {
		t.Run(tc.stage, func(t *testing.T) {
			t.Parallel()

			raw, err := json.Marshal(tc.output)
			requireNoError(t, err, "marshal candidate: %v")

			for _, adapter := range adapters {
				if adapter.name == tc.stage {
					checkContractCase(t, adapter, advertisedSchema(t, adapter), contractCase{"marshaled", string(raw), true, false})

					return
				}
			}

			t.Fatal("missing adapter")
		})
	}
}

type compatibilityOutput struct {
	Output any `json:"output"`
}

type compatibilityCase struct {
	stage  string
	output any
}

func marshaledCompatibilityCandidates() []compatibilityCase {
	observed := time.Date(2026, time.October, 13, 12, 0, 0, 0, time.UTC)
	until := observed.Add(time.Hour)
	ref := memory.RevisionRef{ItemID: "item", RevisionID: "revision"}
	cases := []compatibilityCase{
		{"atoms",
			compatibilityOutput{Output: []memory.AtomCandidate{{Category: memory.AtomCategoryDecision,
				Text:     "A decision",
				Relation: memory.CandidateRelationCoexist,
				Target:   &ref}}}},

		{"canonical",
			compatibilityOutput{Output: []memory.SemanticCandidate{{Kind: memory.MemoryKindEvent,
				Subject:    "Ada",
				Predicate:  "visited",
				Qualifiers: []string{"historical"},
				Memory: memory.MemoryCandidate{
					Kind:      memory.MemoryKindEvent,
					Statement: "Ada visited Bishkek",
					Temporal: memory.Temporal{ObservedAt: observed,
						ValidFrom:         &observed,
						ValidUntil:        &until,
						EventAt:           &observed,
						ExpiresAt:         &until,
						TemporalText:      "today",
						TemporalPrecision: "day",
						Timezone:          "UTC"},

					Evidence: []memory.EvidenceRef{{SourceID: "source",
						MessageID:     "message",
						Role:          memory.MessageRoleTool,
						StartByte:     0,
						EndByte:       4294967295,
						AssertionMode: memory.AssertionModeTrustedTool,
						TextDigest:    "digest"}},
					Sensitivity: memory.SensitivitySensitive,
					Retention:   memory.RetentionClassEphemeral,
				}}}}},
		{"scenarios",
			compatibilityOutput{Output: []memory.ScenarioCandidate{{TopicKey: "home",
				Title:      "Home",
				Summary:    "Bishkek",
				Atoms:      []memory.RevisionRef{ref},
				Supersedes: &ref}}}},

		{"profile",
			compatibilityOutput{Output: memory.ProfileCandidate{Disposition: memory.ProfileDispositionUpsert,
				Summary:    "Bishkek",
				Atoms:      []memory.RevisionRef{ref},
				Scenarios:  []memory.RevisionRef{ref},
				Supersedes: &ref}}},
	}

	return cases
}
