package storefixture_test

import (
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestPublicTraceMixedSourcePrivacy(t *testing.T) {
	for _, sensitive := range []bool{false, true} {
		name := "standard"
		if sensitive {
			name = "sensitive"
		}

		t.Run(name, func(t *testing.T) {
			forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, _ mutationProbe) {
				m := nativeMutation(t)
				addTraceSecondSource(t, &m, sensitive)

				if _, err := store.ApplyCanonicalMutation(t.Context(), m); err != nil {
					t.Fatal(err)
				}

				got, err := publicTraceReader(t, f.Store).Trace(t.Context(), traceRequest(m))
				if sensitive {
					assertEmptyTrace(t, got, err)

					return
				}

				if err != nil || len(got.Sources) != 2 || len(got.Revisions) != 1 {
					t.Fatalf("mixed-source closure=%+v err=%v", got, err)
				}
			})
		})
	}
}

func addTraceSecondSource(t *testing.T, m *memory.CanonicalMutation, sensitive bool) {
	t.Helper()

	turn, err := memory.NewTurn(m.Scope, memory.ConversationRef{ConversationID: "other", AgentConversationID: "agent"},
		"other-turn", m.Operation.CommittedAt, "second source", "second answer")
	if err != nil {
		t.Fatal(err)
	}

	data := privacyHistoryHeader(t, turn)

	source := memory.CanonicalSourceRecord{SourceID: turn.SourceID, Scope: m.Scope,
		Payload: privacyRef("second-source", data), Sensitivity: memory.SensitivityStandard,
		Retention: memory.RetentionClassStandard}
	if sensitive {
		source.Sensitivity = memory.SensitivitySensitive
	}

	m.Sources = append(m.Sources, source)
	m.Payloads = append(m.Payloads, memory.CanonicalPayload{Ref: source.Payload, Data: data})

	for _, message := range turn.Messages {
		ref := privacyRef(message.MessageID, []byte(message.Text))
		m.Messages = append(m.Messages, memory.MessageRecord{MessageID: message.MessageID,
			SourceID: source.SourceID, Role: message.Role, Payload: ref})
		m.Payloads = append(m.Payloads, memory.CanonicalPayload{Ref: ref, Data: []byte(message.Text)})
	}

	ref, err := memory.NewEvidenceRef(source.SourceID, turn.Messages[0].MessageID, memory.MessageRoleUser,
		turn.Messages[0].Text, 0, uint32(len(turn.Messages[0].Text)), memory.AssertionModeUser)
	if err != nil {
		t.Fatal(err)
	}

	m.Revisions[0].Evidence = append(m.Revisions[0].Evidence, ref)
}
