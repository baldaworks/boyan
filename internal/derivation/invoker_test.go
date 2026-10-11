package derivation //nolint:testpackage // Exercises defaultRuntimeAppName within the package.

import (
	"context"
	"errors"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/structuredderive"
	boyan "github.com/baldaworks/boyan/pkg/memory"
	"github.com/normahq/runtime/v2/agentconfig"
	"github.com/normahq/runtime/v2/agentfactory"
	runtimeconfig "github.com/normahq/runtime/v2/appconfig"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/genai"
)

type fakeFactory struct {
	validateErr error
	buildErr    error
	requests    []agentfactory.BuildRequest
	builds      int
}

func (f *fakeFactory) ValidateAgent(string) error { return f.validateErr }

func (f *fakeFactory) Build(_ context.Context, req agentfactory.BuildRequest) (adkagent.Agent, error) {
	f.requests = append(f.requests, req)
	if f.buildErr != nil {
		return nil, f.buildErr
	}

	f.builds++

	return adkagent.New(adkagent.Config{Name: req.Name, Description: req.Description})
}

type fakeRunner struct {
	run func(
		ctx context.Context,
		userID, stateID string,
		msg *genai.Content,
		cfg adkagent.RunConfig,
	) iter.Seq2[*runEvent, error]
}

func (f fakeRunner) Run(
	ctx context.Context,
	userID, stateID string,
	msg *genai.Content,
	cfg adkagent.RunConfig,
	_ ...runner.RunOption,
) iter.Seq2[*runEvent, error] {
	return f.run(ctx, userID, stateID, msg, cfg)
}

func testConfig(t *testing.T) Config {
	t.Helper()

	return Config{
		Runtime: runtimeconfig.RuntimeConfig{
			Providers: map[string]agentconfig.Config{
				"memory": {
					Type:     agentconfig.AgentTypeCodexACP,
					CodexACP: &agentconfig.ACPConfig{Model: "memory-fast"},
				},
			},
		},
		ProviderID: "memory",
		WorkingDir: t.TempDir(),
	}
}

func TestDedicatedRuntimeBuildRequestUsesProviderWithoutMCPOrReasoningOverride(t *testing.T) {
	t.Parallel()

	req, err := dedicatedRuntimeBuildRequest(" memory ", " /tmp/boyan ", " derive facts ")
	requireNoError(t, err, "dedicatedRuntimeBuildRequest() error = %v")

	if req.AgentID != "memory" {
		t.Fatalf("req.AgentID = %q, want memory", req.AgentID)
	}

	if req.Name != "memory-boyan-derivation" {
		t.Fatalf("req.Name = %q, want memory-boyan-derivation", req.Name)
	}

	if req.WorkingDirectory != "/tmp/boyan" || req.Instruction != "derive facts" {
		t.Fatalf("req = %+v, want trimmed working directory and instruction", req)
	}

	if len(req.MCPServerIDs) != 0 {
		t.Fatalf("req.MCPServerIDs = %#v, want empty", req.MCPServerIDs)
	}

	if req.ReasoningEffort != "" {
		t.Fatalf("req.ReasoningEffort = %q, want empty", req.ReasoningEffort)
	}
}

func TestNewInvokerIsLazyAndCloseIsIdempotent(t *testing.T) {
	t.Parallel()

	factory := &fakeFactory{}

	invoker, err := newInvokerWithFactory(testConfig(
		t,
	), factory, func(
		adkagent.Agent,
		runStateStore,
		string,
	) (
		eventRunner,
		error,
	) {
		return fakeRunner{}, nil
	})
	requireNoError(t, err, "newInvokerWithFactory() error = %v")

	if invoker.runtime != nil {
		t.Fatal("invoker built runtime before first invocation")
	}

	if factory.builds != 0 {
		t.Fatalf("factory.builds = %d, want 0 before invoke", factory.builds)
	}

	requireNoError(t, invoker.Close(context.Background()), "Close() error = %v")

	requireNoError(t, invoker.Close(context.Background()), "second Close() error = %v")
}

func TestInvokeBuildFailureIsRetryableModelError(t *testing.T) {
	t.Parallel()

	invoker, err := newInvokerWithFactory(testConfig(
		t,
	), &fakeFactory{
		buildErr: errors.New(
			"boom",
		),
	}, func(
		adkagent.Agent,
		runStateStore,
		string,
	) (
		eventRunner,
		error,
	) {
		return fakeRunner{}, nil
	})
	requireNoError(t, err, "newInvokerWithFactory() error = %v")

	_, err = invoker.Invoke(context.Background(), structuredderive.StructuredInvocation{
		OperationID:  "operation-1",
		Stage:        "canonical_semantics",
		InputJSON:    []byte(`{"input":"hi"}`),
		OutputSchema: `{"type":"object","properties":{"output":{"type":"array"}},"required":["output"]}`,
	})

	code, class, ok := boyan.ClassifyError(err)
	if !ok || code != boyan.CodeModelFailure || class != boyan.ErrorClassRetryable {
		t.Fatalf(
			"ClassifyError() = %q, %q, %v; want %q, retryable, true",
			code,
			class,
			ok,
			boyan.CodeModelFailure,
		)
	}
}

func TestInvokeTimeoutMapsToStableTimeout(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t)
	cfg.Timeout = 10 * time.Millisecond

	invoker, err := newInvokerWithFactory(cfg, &fakeFactory{}, func(
		adkagent.Agent,
		runStateStore,
		string,
	) (
		eventRunner,
		error,
	) {
		return fakeRunner{
			run: func(
				ctx context.Context,
				userID, stateID string,
				msg *genai.Content,
				cfg adkagent.RunConfig,
			) iter.Seq2[*runEvent, error] {
				return func(yield func(*runEvent, error) bool) {
					<-ctx.Done()
					yield(nil, ctx.Err())
				}
			},
		}, nil
	})
	requireNoError(t, err, "newInvokerWithFactory() error = %v")

	_, err = invoker.Invoke(context.Background(), structuredderive.StructuredInvocation{
		OperationID:  "operation-1",
		Stage:        "canonical_semantics",
		InputJSON:    []byte(`{"input":"hi"}`),
		OutputSchema: `{"type":"object","properties":{"output":{"type":"array"}},"required":["output"]}`,
	})

	code, class, ok := boyan.ClassifyError(err)
	if !ok || code != boyan.CodeTimeout || class != boyan.ErrorClassRetryable {
		t.Fatalf(
			"ClassifyError() = %q, %q, %v; want %q, retryable, true",
			code,
			class,
			ok,
			boyan.CodeTimeout,
		)
	}
}

func TestDeriverRejectsMalformedOutputFromRuntimeInvoker(t *testing.T) {
	t.Parallel()

	invoker, err := newInvokerWithFactory(testConfig(
		t,
	), &fakeFactory{}, func(
		_ adkagent.Agent,
		_ runStateStore,
		_ string,
	) (
		eventRunner,
		error,
	) {
		return fakeRunner{
			run: func(
				ctx context.Context,
				userID, stateID string,
				msg *genai.Content,
				cfg adkagent.RunConfig,
			) iter.Seq2[*runEvent, error] {
				return func(yield func(*runEvent, error) bool) {
					yield(&runEvent{
						LLMResponse: model.LLMResponse{
							Content: &genai.Content{Parts: []*genai.Part{{Text: "not-json"}}},
						},
					}, nil)
				}
			},
		}, nil
	})
	requireNoError(t, err, "newInvokerWithFactory() error = %v")

	deriver, err := structuredderive.NewDeriver(invoker)
	requireNoError(t, err, "NewDeriver() error = %v")

	turn, err := boyan.NewTurn(
		boyan.Scope{Key: "telegram:1", Kind: boyan.ScopeKindPersonal},
		boyan.ConversationRef{ConversationID: "conversation-1", AgentConversationID: "agent-1"},
		"turn-1",
		time.Date(2026, time.August, 9, 10, 0, 0, 0, time.UTC),
		"remember",
		"stored",
	)
	requireNoError(t, err, "NewTurn() error = %v")

	_, err = deriver.ExtractCanonicalSemantics(context.Background(), boyan.CanonicalExtractionRequest{
		SchemaVersion: boyan.CanonicalSchemaVersionV1,
		Derivation:    boyan.LegacyDerivationRef(),
		Turn:          turn,
	})

	code, _, ok := boyan.ClassifyError(err)
	if !ok || code != boyan.CodeInvalidDerived {
		t.Fatalf("ClassifyError() = %q, %v; want %q, true", code, ok, boyan.CodeInvalidDerived)
	}
}

func TestInvokeUsesShortLivedInMemoryRuntimeState(t *testing.T) {
	t.Parallel()

	var (
		stateStore runStateStore
		runStateID string
	)

	invoker, err := newInvokerWithFactory(
		testConfig(t),
		&fakeFactory{},
		capturedRuntimeStateRunner(&stateStore, &runStateID),
	)
	requireNoError(t, err, "newInvokerWithFactory() error = %v")

	deriver, err := structuredderive.NewDeriver(invoker)
	requireNoError(t, err, "NewDeriver() error = %v")

	turn, err := boyan.NewTurn(
		boyan.Scope{Key: "telegram:2", Kind: boyan.ScopeKindPersonal},
		boyan.ConversationRef{ConversationID: "conversation-2", AgentConversationID: "agent-2"},
		"turn-2",
		time.Date(2026, time.August, 9, 10, 0, 0, 0, time.UTC),
		"Ada",
		"Ada lives in Bishkek",
	)
	requireNoError(t, err, "NewTurn() error = %v")

	items, err := deriver.ExtractCanonicalSemantics(context.Background(), boyan.CanonicalExtractionRequest{
		SchemaVersion: boyan.CanonicalSchemaVersionV1,
		Derivation:    boyan.LegacyDerivationRef(),
		Turn:          turn,
	})
	requireNoError(t, err, "ExtractCanonicalSemantics() error = %v")

	if len(items) != 1 {
		t.Fatalf("items = %#v, want one output item", items)
	}

	if strings.TrimSpace(runStateID) == "" {
		t.Fatal("runner runtime state ID was empty")
	}

	if err := getRunState(context.Background(), stateStore, defaultRuntimeAppName, runStateID); err == nil {
		t.Fatal("derivation runtime state still exists after invoke")
	}
}

func capturedRuntimeStateRunner(stateStore *runStateStore, runStateID *string) runnerBuilder {
	return func(
		_ adkagent.Agent,
		store runStateStore,
		_ string,
	) (
		eventRunner,
		error,
	) {
		*stateStore = store

		return fakeRunner{
			run: func(
				ctx context.Context,
				userID, currentStateID string,
				msg *genai.Content,
				cfg adkagent.RunConfig,
			) iter.Seq2[*runEvent, error] {
				*runStateID = currentStateID

				return func(yield func(*runEvent, error) bool) {
					yield(&runEvent{
						LLMResponse: model.LLMResponse{
							Content: &genai.Content{
								Parts: []*genai.Part{
									{
										Text: ("{\"output\":[{\"Kind\":\"state\",\"Subject\":\"Ada\",\"Predicate\":\"Live" +
											"s In\",\"Memory\":{\"Kind\":\"state\",\"Statement\":\"Ada lives in Bis" +
											"hkek\",\"Temporal\":{\"ObservedAt\":\"2026-08-09T10:00:00Z\"},\"Evid" +
											"ence\":[{\"SourceID\":\"source-1\",\"MessageID\":\"message-1\",\"Role\"" +
											":\"user\",\"StartByte\":0,\"EndByte\":3,\"AssertionMode\":\"user\"}],\"" +
											"Sensitivity\":\"standard\",\"Retention\":\"standard\"}}]}"),
									},
								},
							},
						},
					}, nil)
				}
			},
		}, nil
	}
}
