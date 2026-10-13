package structuredderive_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	derive "github.com/baldaworks/boyan/internal/structuredderive"
	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestBoundaryDerivationPreservesDescriptiveEventTime(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.October, 13, 0, 0, 0, 0, time.UTC)
	scope := m.Scope{Key: "test:event-context", Kind: m.ScopeKindPersonal}
	boundary, err := m.NewBoundary(scope, m.ConversationRef{ConversationID: "c", AgentConversationID: "a"},
		"close", m.BoundaryReasonClose, at)
	requireNoError(t, err, "boundary: %v")

	for _, event := range []time.Time{at.Add(-24 * time.Hour), at.Add(24 * time.Hour)} {
		temporal := m.Temporal{ObservedAt: at,
			EventAt:           &event,
			TemporalText:      "dated visit",
			TemporalPrecision: "day",
			Timezone:          "UTC"}
		view := m.ScopeView{SchemaVersion: m.DerivedSchemaVersionV1, Scope: scope, Atoms: []m.Atom{{Temporal: &temporal}}}
		invoker := &contractInvoker{raw: []byte(`{"output":[]}`)}
		deriver, err := derive.NewDeriver(invoker)
		requireNoError(t, err, "deriver: %v")
		_, err = deriver.SynthesizeScenarios(context.Background(), m.ScenarioSynthesisRequest{
			Boundary: boundary, Derivation: m.LegacyDerivationRef(), View: view})
		requireNoError(t, err, "scenario: %v")
		checkDescriptiveTemporalInput(t, invoker.last, temporal)
		invoker.raw = []byte(`{"output":{"disposition":"skip"}}`)
		_, err = deriver.SynthesizeProfile(context.Background(), m.ProfileSynthesisRequest{
			Boundary: boundary, Derivation: m.LegacyDerivationRef(), View: view})
		requireNoError(t, err, "profile: %v")
		checkDescriptiveTemporalInput(t, invoker.last, temporal)
	}
}

func checkDescriptiveTemporalInput(t *testing.T, invocation derive.StructuredInvocation, want m.Temporal) {
	t.Helper()

	var input struct {
		View m.ScopeView `json:"view"`
	}
	requireNoError(t, json.Unmarshal(invocation.InputJSON, &input), "input: %v")

	got := input.View.Atoms[0].Temporal
	if got == nil || got.EventAt == nil || !got.EventAt.Equal(*want.EventAt) || got.TemporalText != want.TemporalText ||
		got.TemporalPrecision != want.TemporalPrecision || got.Timezone != want.Timezone {
		t.Errorf("descriptive temporal context lost: got=%+v want=%+v", got, want)
	}

	if !strings.Contains(invocation.Instruction, "historical and planned") ||
		!strings.Contains(invocation.Instruction, "undated current fact") {
		t.Errorf("provider instruction lacks dated-event semantics: %s", invocation.Instruction)
	}
}
