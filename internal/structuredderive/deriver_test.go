package structuredderive_test

import structuredderive "github.com/baldaworks/boyan/internal/structuredderive"

import (
	"context"
	"strings"
	"testing"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

type scriptedInvoker struct {
	output []byte
	last   structuredderive.StructuredInvocation
}

func (s *scriptedInvoker) Invoke(_ context.Context, request structuredderive.StructuredInvocation) ([]byte, error) {
	s.last = request

	return append([]byte(nil), s.output...), nil
}

func (*scriptedInvoker) Close(context.Context) error { return nil }

func TestDeriverExtractCanonicalSemanticsUsesNeutralStructuredBoundary(t *testing.T) {
	t.Parallel()

	invoker := &scriptedInvoker{
		output: []byte(`{"output":[{"Kind":"state","Subject":"Ada","Predicate":"Lives In","Memory":{"kind":"s` +
			`tate","statement":"Ada lives in Bishkek","temporal":{"observed_at":"2026-08-03T16:00:` +
			`00Z"},"evidence":[{"source_id":"source-1","message_id":"message-1","role":"user","sta` +
			`rt_byte":0,"end_byte":3,"assertion_mode":"user"}],"sensitivity":"standard","retention` +
			`":"standard"}}]}`),
	}

	deriver, err := structuredderive.NewDeriver(invoker)
	requireNoError(t, err, "NewDeriver() error = %v")

	turn, err := boyan.NewTurn(
		boyan.Scope{Key: "telegram:1", Kind: boyan.ScopeKindPersonal},
		boyan.ConversationRef{ConversationID: "conversation-1", AgentConversationID: "agent-1"},
		"turn-1", time.Date(2026, time.August, 3, 16, 0, 0, 0, time.UTC), "Ada", "Ada lives in Bishkek",
	)
	requireNoError(t, err, "NewTurn() error = %v")

	items, err := deriver.ExtractCanonicalSemantics(context.Background(), boyan.CanonicalExtractionRequest{
		SchemaVersion: boyan.CanonicalSchemaVersionV1,
		Derivation:    boyan.LegacyDerivationRef(),
		Turn:          turn,
	})
	requireNoError(t, err, "ExtractCanonicalSemantics() error = %v")

	if len(items) != 1 || items[0].Memory.Statement != "Ada lives in Bishkek" {
		t.Fatalf("canonical semantics = %#v", items)
	}

	if invoker.last.Stage != "canonical_semantics" ||
		!strings.Contains(invoker.last.Instruction, "Boyan derivation worker") {
		t.Fatalf("structured invocation = %#v", invoker.last)
	}
}

func TestDeriverRejectsMalformedStructuredOutput(t *testing.T) {
	t.Parallel()

	deriver, err := structuredderive.NewDeriver(&scriptedInvoker{output: []byte("not-json")})
	requireNoError(t, err, "NewDeriver() error = %v")

	turn, err := boyan.NewTurn(
		boyan.Scope{Key: "telegram:2", Kind: boyan.ScopeKindPersonal},
		boyan.ConversationRef{ConversationID: "conversation-2", AgentConversationID: "agent-2"},
		"turn-2", time.Date(2026, time.August, 3, 16, 0, 0, 0, time.UTC), "remember", "stored",
	)
	requireNoError(t, err, "NewTurn() error = %v")

	_, err = deriver.ExtractCanonicalSemantics(context.Background(), boyan.CanonicalExtractionRequest{
		SchemaVersion: boyan.CanonicalSchemaVersionV1,
		Turn:          turn,
		Derivation:    boyan.LegacyDerivationRef(),
	})
	if code, _, ok := boyan.ClassifyError(err); !ok || code != boyan.CodeInvalidDerived {
		t.Fatalf("error = %v, code = %q, classified = %v", err, code, ok)
	}
}
