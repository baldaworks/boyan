package structuredderive_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	derive "github.com/baldaworks/boyan/internal/structuredderive"
	m "github.com/baldaworks/boyan/pkg/memory"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type contractInvoker struct {
	raw  []byte
	err  error
	last derive.StructuredInvocation
}

func (i *contractInvoker) Invoke(_ context.Context, request derive.StructuredInvocation) ([]byte, error) {
	i.last = request

	return i.raw, i.err
}

func (*contractInvoker) Close(context.Context) error { return nil }

type adapterContract struct {
	name      string
	empty     string
	populated string
	call      func(*derive.Deriver) error
}

func adapterContracts(t *testing.T) []adapterContract {
	t.Helper()

	scope := m.Scope{Key: "test:wire-contract", Kind: m.ScopeKindPersonal}
	conversation := m.ConversationRef{ConversationID: "conversation", AgentConversationID: "agent"}
	at := time.Date(2026, time.October, 12, 0, 0, 0, 0, time.UTC)
	turn, err := m.NewTurn(scope, conversation, "turn", at, "remember", "OK")
	requireNoError(t, err, "turn: %v")
	boundary, err := m.NewBoundary(scope, conversation, "transition", m.BoundaryReasonClose, at)
	requireNoError(t, err, "boundary: %v")

	derivation := m.LegacyDerivationRef()

	return []adapterContract{
		{name: "atoms", empty: `{"output":[]}`,
			populated: `{"output":[{"category":"fact","text":"Ada","relation":"new"}]}`,
			call: func(d *derive.Deriver) error {
				_, callErr := d.ExtractAtoms(context.Background(), m.AtomExtractionRequest{
					SchemaVersion: m.DerivedSchemaVersionV1, Derivation: derivation, Turn: turn})

				return callErr
			}},
		{name: "canonical", empty: `{"output":[]}`,
			populated: `{"output":[{"Kind":"state","Subject":"Ada","Predicate":"home","Memory":{"ki` +
				`nd":"state` +
				`","statement":"Ada is home","temporal":{"observed_at":"2026-10-12T00:00:00Z` +
				`"},"eviden` +
				`ce":[],"sensitivity":"standard","retention":"standard"}}]}`,
			call: func(d *derive.Deriver) error {
				_, callErr := d.ExtractCanonicalSemantics(context.Background(), m.CanonicalExtractionRequest{
					SchemaVersion: m.CanonicalSchemaVersionV1, Derivation: derivation, Turn: turn})

				return callErr
			}},
		{name: "scenarios", empty: `{"output":[]}`,
			populated: `{"output":[{"topic_key":"home","title":"Home","summary":"Ada","atoms":[{"it` +
				`em_id":"i"` +
				`,"revision_id":"r"}]}]}`,
			call: func(d *derive.Deriver) error {
				_, callErr := d.SynthesizeScenarios(context.Background(), m.ScenarioSynthesisRequest{
					SchemaVersion: m.DerivedSchemaVersionV1, Derivation: derivation, Boundary: boundary})

				return callErr
			}},
		{name: "profile", empty: `{"output":{"disposition":"skip"}}`,
			populated: `{"output":{"disposition":"upsert","summary":"Ada","atoms":[{"item_id":"i","` +
				`revision_id":"r"}]}}`,
			call: func(d *derive.Deriver) error {
				_, callErr := d.SynthesizeProfile(context.Background(), m.ProfileSynthesisRequest{
					SchemaVersion: m.DerivedSchemaVersionV1, Derivation: derivation, Boundary: boundary})

				return callErr
			}},
	}
}

func TestAllAdaptersRejectMalformedEnvelopes(t *testing.T) {
	t.Parallel()

	for _, adapter := range adapterContracts(t) {
		t.Run(adapter.name, func(t *testing.T) {
			for _, raw := range []string{
				`{}`, `null`, `{"output":null}`, `{"Output":null}`,
				strings.TrimSuffix(adapter.empty, "}") + `,"output":null}`,
				strings.TrimSuffix(adapter.empty, "}") + `,"\u006futput":null}`,
				strings.TrimSuffix(adapter.empty, "}") + `,"secret":"private-provider-text"}`,
				adapter.empty + ` {}`,
			} {
				t.Run(raw, func(t *testing.T) {
					d, err := derive.NewDeriver(&contractInvoker{raw: []byte(raw)})
					requireNoError(t, err, "deriver: %v")
					err = adapter.call(d)
					requireContractError(t, err, m.CodeInvalidDerived, false)

					if strings.Contains(err.Error(), "private-provider-text") {
						t.Fatal("invalid output error disclosed provider content")
					}
				})
			}
		})
	}
}

func TestAllAdaptersPreserveExplicitEmptySuccess(t *testing.T) {
	t.Parallel()

	for _, adapter := range adapterContracts(t) {
		t.Run(adapter.name, func(t *testing.T) {
			for _, raw := range []string{adapter.empty, adapter.populated} {
				invoker := &contractInvoker{raw: []byte(raw)}
				d, err := derive.NewDeriver(invoker)
				requireNoError(t, err, "deriver: %v")
				requireNoError(t, adapter.call(d), "valid response: %v")

				document, err := jsonschema.UnmarshalJSON(strings.NewReader(invoker.last.OutputSchema))
				requireNoError(t, err, "advertised schema: %v")

				compiler := jsonschema.NewCompiler()
				compiler.AssertFormat()
				requireNoError(t, compiler.AddResource("urn:boyan:adapter", document), "schema resource: %v")
				schema, err := compiler.Compile("urn:boyan:adapter")
				requireNoError(t, err, "compile supplied schema: %v")
				value, err := jsonschema.UnmarshalJSON(strings.NewReader(raw))
				requireNoError(t, err, "fixture: %v")
				requireNoError(t, schema.Validate(value), "supplied schema rejects valid fixture: %v")
			}
		})
	}
}

func TestAllAdaptersPreserveProviderAndLimitClassification(t *testing.T) {
	t.Parallel()

	for _, adapter := range adapterContracts(t) {
		t.Run(adapter.name, func(t *testing.T) {
			for _, test := range []struct {
				name      string
				invoker   *contractInvoker
				maxOutput int
				code      m.ErrorCode
				retryable bool
			}{
				{name: "provider", invoker: &contractInvoker{err: errors.New("private-provider-text")},
					code: m.CodeModelFailure, retryable: true},
				{name: "typed-provider", invoker: &contractInvoker{err: m.PermanentError(m.CodeLimitExceeded, "bounded", nil)},
					code: m.CodeLimitExceeded},
				{name: "empty-bytes", invoker: &contractInvoker{}, code: m.CodeLimitExceeded},
				{name: "oversized", invoker: &contractInvoker{raw: []byte(adapter.empty)},
					maxOutput: 1, code: m.CodeLimitExceeded},
			} {
				t.Run(test.name, func(t *testing.T) {
					d, err := derive.NewDeriverWithConfig(derive.DeriverConfig{
						Invoker: test.invoker, MaxOutputBytes: test.maxOutput})
					requireNoError(t, err, "deriver: %v")
					err = adapter.call(d)
					requireContractError(t, err, test.code, test.retryable)

					if strings.Contains(err.Error(), "private-provider-text") {
						t.Fatal("provider error disclosed provider content")
					}
				})
			}
		})
	}
}

func requireContractError(t *testing.T, err error, want m.ErrorCode, retryable bool) {
	t.Helper()

	code, _, classified := m.ClassifyError(err)

	gotRetryable := m.IsRetryable(err)
	if !classified || code != want || gotRetryable != retryable {
		t.Fatalf("error=%v code=%q retryable=%v; want code=%q retryable=%v", err, code, gotRetryable, want, retryable)
	}
}
