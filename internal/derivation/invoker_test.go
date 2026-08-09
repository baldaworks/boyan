package derivation

import (
	"context"
	"errors"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/baldaworks/boyan"
	portableapp "github.com/baldaworks/boyan/app"
	"github.com/normahq/runtime/v2/agentconfig"
	"github.com/normahq/runtime/v2/agentfactory"
	runtimeconfig "github.com/normahq/runtime/v2/appconfig"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
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
	run func(ctx context.Context, userID, sessionID string, msg *genai.Content, cfg adkagent.RunConfig) iter.Seq2[*adksession.Event, error]
}

func (f fakeRunner) Run(ctx context.Context, userID, sessionID string, msg *genai.Content, cfg adkagent.RunConfig, _ ...runner.RunOption) iter.Seq2[*adksession.Event, error] {
	return f.run(ctx, userID, sessionID, msg, cfg)
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
	if err != nil {
		t.Fatalf("dedicatedRuntimeBuildRequest() error = %v", err)
	}
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
	invoker, err := newInvokerWithFactory(testConfig(t), factory, func(adkagent.Agent, adksession.Service, string) (eventRunner, error) {
		return fakeRunner{}, nil
	})
	if err != nil {
		t.Fatalf("newInvokerWithFactory() error = %v", err)
	}
	if invoker.runtime != nil {
		t.Fatal("invoker built runtime before first invocation")
	}
	if factory.builds != 0 {
		t.Fatalf("factory.builds = %d, want 0 before invoke", factory.builds)
	}
	if err := invoker.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := invoker.Close(context.Background()); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestInvokeBuildFailureIsRetryableModelError(t *testing.T) {
	t.Parallel()

	invoker, err := newInvokerWithFactory(testConfig(t), &fakeFactory{buildErr: errors.New("boom")}, func(adkagent.Agent, adksession.Service, string) (eventRunner, error) {
		return fakeRunner{}, nil
	})
	if err != nil {
		t.Fatalf("newInvokerWithFactory() error = %v", err)
	}
	_, err = invoker.Invoke(context.Background(), portableapp.StructuredInvocation{
		OperationID:  "operation-1",
		Stage:        "canonical_semantics",
		InputJSON:    []byte(`{"input":"hi"}`),
		OutputSchema: `{"type":"object","properties":{"output":{"type":"array"}},"required":["output"]}`,
	})
	code, class, ok := boyan.ClassifyError(err)
	if !ok || code != boyan.CodeModelFailure || class != boyan.ErrorClassRetryable {
		t.Fatalf("ClassifyError() = %q, %q, %v; want %q, retryable, true", code, class, ok, boyan.CodeModelFailure)
	}
}

func TestInvokeTimeoutMapsToStableTimeout(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t)
	cfg.Timeout = 10 * time.Millisecond
	invoker, err := newInvokerWithFactory(cfg, &fakeFactory{}, func(adkagent.Agent, adksession.Service, string) (eventRunner, error) {
		return fakeRunner{
			run: func(ctx context.Context, userID, sessionID string, msg *genai.Content, cfg adkagent.RunConfig) iter.Seq2[*adksession.Event, error] {
				return func(yield func(*adksession.Event, error) bool) {
					<-ctx.Done()
					yield(nil, ctx.Err())
				}
			},
		}, nil
	})
	if err != nil {
		t.Fatalf("newInvokerWithFactory() error = %v", err)
	}
	_, err = invoker.Invoke(context.Background(), portableapp.StructuredInvocation{
		OperationID:  "operation-1",
		Stage:        "canonical_semantics",
		InputJSON:    []byte(`{"input":"hi"}`),
		OutputSchema: `{"type":"object","properties":{"output":{"type":"array"}},"required":["output"]}`,
	})
	code, class, ok := boyan.ClassifyError(err)
	if !ok || code != boyan.CodeTimeout || class != boyan.ErrorClassRetryable {
		t.Fatalf("ClassifyError() = %q, %q, %v; want %q, retryable, true", code, class, ok, boyan.CodeTimeout)
	}
}

func TestDeriverRejectsMalformedOutputFromRuntimeInvoker(t *testing.T) {
	t.Parallel()

	invoker, err := newInvokerWithFactory(testConfig(t), &fakeFactory{}, func(_ adkagent.Agent, svc adksession.Service, _ string) (eventRunner, error) {
		return fakeRunner{
			run: func(ctx context.Context, userID, sessionID string, msg *genai.Content, cfg adkagent.RunConfig) iter.Seq2[*adksession.Event, error] {
				return func(yield func(*adksession.Event, error) bool) {
					yield(&adksession.Event{
						LLMResponse: model.LLMResponse{
							Content: &genai.Content{Parts: []*genai.Part{{Text: "not-json"}}},
						},
					}, nil)
				}
			},
		}, nil
	})
	if err != nil {
		t.Fatalf("newInvokerWithFactory() error = %v", err)
	}
	deriver, err := portableapp.NewDeriver(invoker)
	if err != nil {
		t.Fatalf("NewDeriver() error = %v", err)
	}
	turn, err := boyan.NewTurn(
		boyan.Scope{Key: "telegram:1", Kind: boyan.ScopeKindPersonal},
		boyan.SessionRef{SessionID: "session-1", AgentSessionID: "agent-1"},
		"turn-1",
		time.Date(2026, time.August, 9, 10, 0, 0, 0, time.UTC),
		"remember",
		"stored",
	)
	if err != nil {
		t.Fatalf("NewTurn() error = %v", err)
	}
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

func TestInvokeUsesShortLivedInMemorySessions(t *testing.T) {
	t.Parallel()

	var sessionSvc adksession.Service
	var sessionID string
	invoker, err := newInvokerWithFactory(testConfig(t), &fakeFactory{}, func(_ adkagent.Agent, svc adksession.Service, _ string) (eventRunner, error) {
		sessionSvc = svc
		return fakeRunner{
			run: func(ctx context.Context, userID, currentSessionID string, msg *genai.Content, cfg adkagent.RunConfig) iter.Seq2[*adksession.Event, error] {
				sessionID = currentSessionID
				return func(yield func(*adksession.Event, error) bool) {
					yield(&adksession.Event{
						LLMResponse: model.LLMResponse{
							Content: &genai.Content{Parts: []*genai.Part{{Text: `{"output":[{"Kind":"state","Subject":"Ada","Predicate":"Lives In","Memory":{"Kind":"state","Statement":"Ada lives in Bishkek","Temporal":{"ObservedAt":"2026-08-09T10:00:00Z"},"Evidence":[{"SourceID":"source-1","MessageID":"message-1","Role":"user","StartByte":0,"EndByte":3,"AssertionMode":"user"}],"Sensitivity":"standard","Retention":"standard"}}]}`}}},
						},
					}, nil)
				}
			},
		}, nil
	})
	if err != nil {
		t.Fatalf("newInvokerWithFactory() error = %v", err)
	}
	deriver, err := portableapp.NewDeriver(invoker)
	if err != nil {
		t.Fatalf("NewDeriver() error = %v", err)
	}
	turn, err := boyan.NewTurn(
		boyan.Scope{Key: "telegram:2", Kind: boyan.ScopeKindPersonal},
		boyan.SessionRef{SessionID: "session-2", AgentSessionID: "agent-2"},
		"turn-2",
		time.Date(2026, time.August, 9, 10, 0, 0, 0, time.UTC),
		"Ada",
		"Ada lives in Bishkek",
	)
	if err != nil {
		t.Fatalf("NewTurn() error = %v", err)
	}
	items, err := deriver.ExtractCanonicalSemantics(context.Background(), boyan.CanonicalExtractionRequest{
		SchemaVersion: boyan.CanonicalSchemaVersionV1,
		Derivation:    boyan.LegacyDerivationRef(),
		Turn:          turn,
	})
	if err != nil {
		t.Fatalf("ExtractCanonicalSemantics() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %#v, want one output item", items)
	}
	if strings.TrimSpace(sessionID) == "" {
		t.Fatal("runner session ID was empty")
	}
	if _, err := sessionSvc.Get(context.Background(), &adksession.GetRequest{
		AppName:   defaultRuntimeAppName,
		UserID:    defaultDerivationUser,
		SessionID: sessionID,
	}); err == nil {
		t.Fatal("derivation session still exists after invoke")
	}
}
