package derivation //nolint:testpackage // Exercises real Invoker/Deriver and ADK state ownership.

import (
	"context"
	"errors"
	"iter"
	"sync/atomic"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/structuredderive"
	m "github.com/baldaworks/boyan/pkg/memory"
	"github.com/normahq/runtime/v2/agentfactory"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

type isolatedRunCapture struct {
	store   runStateStore
	app, id string
}
type isolatedInvocation func(context.Context) error

func TestInvocationStateIsolationAcrossOverlappingAttempts(t *testing.T) {
	for _, kind := range []string{"same-export-cross-kind", "identical-input", "distinct-export"} {
		for _, outcome := range []string{"success", "failure", "cancel"} {
			t.Run(kind+"/"+outcome, func(t *testing.T) { assertInvocationOverlap(t, kind, outcome) })
		}
	}
}

func assertInvocationOverlap(t *testing.T, kind, outcome string) {
	t.Helper()

	captures := make(chan isolatedRunCapture, 2)
	release := make(chan struct{})
	factory := &isolatedProviderFactory{captures: captures, release: release, outcome: outcome}
	invoker, err := newInvokerWithFactory(testConfig(t), factory, defaultRunnerBuilder)
	requireNoError(t, err, "invoker: %v")
	t.Cleanup(func() { _ = invoker.Close(context.Background()) })

	runtime, err := invoker.ensureRuntime(context.Background())
	requireNoError(t, err, "runtime: %v")

	factory.store = runtime.stateStore
	firstInvoke, secondInvoke := isolatedInvocations(t, invoker, kind)

	firstDone := make(chan error, 1)
	go func() { firstDone <- firstInvoke(context.Background()) }()

	first := <-captures
	requireNoError(t, getRunState(context.Background(), first.store, first.app, first.id), "first created state: %v")

	secondCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	secondDone := make(chan error, 1)
	go func() { secondDone <- secondInvoke(secondCtx) }()

	second := <-captures

	if outcome == "cancel" {
		cancel()
	}

	secondErr := <-secondDone
	assertIsolatedSecondOutcome(t, secondErr, outcome)

	firstErr := getRunState(context.Background(), first.store, first.app, first.id)
	secondErr = getRunState(context.Background(), second.store, second.app, second.id)

	close(release)

	resumedErr := <-firstDone
	if first.id == second.id || firstErr != nil || secondErr == nil || resumedErr != nil {
		t.Fatalf("invocation states interfered: sameID=%v first=%v second=%v resumed=%v",
			first.id == second.id, firstErr, secondErr, resumedErr)
	}

	if err := getRunState(context.Background(), first.store, first.app, first.id); err == nil {
		t.Fatal("first owned state survived completion")
	}
}

type isolatedProviderFactory struct {
	fakeFactory

	store    runStateStore
	captures chan<- isolatedRunCapture
	release  <-chan struct{}
	outcome  string
	starts   atomic.Int32
}

func (f *isolatedProviderFactory) Build(_ context.Context, request agentfactory.BuildRequest) (adkagent.Agent, error) {
	return adkagent.New(adkagent.Config{Name: request.Name, Run: f.run})
}

func (f *isolatedProviderFactory) run(ctx adkagent.InvocationContext) iter.Seq2[*session.Event, error] {
	first := f.starts.Add(1) == 1

	return func(yield func(*session.Event, error) bool) {
		id, app := ctx.Session().ID(), ctx.Session().AppName()
		f.captures <- isolatedRunCapture{store: f.store, app: app, id: id}

		if first {
			<-f.release
		}

		if err := getRunState(context.Background(), f.store, app, id); err != nil {
			yield(nil, err)

			return
		}

		if !first && f.outcome == "failure" {
			yield(nil, errors.New("provider failed"))

			return
		}

		if !first && f.outcome == "cancel" {
			<-ctx.Done()
			yield(nil, ctx.Err())

			return
		}

		event := session.NewEvent(context.Background(), ctx.InvocationID())
		event.Content = genai.NewContentFromText(`{"output":[]}`, genai.RoleModel)
		event.TurnComplete = true
		yield(event, nil)
	}
}

func assertIsolatedSecondOutcome(t *testing.T, err error, outcome string) {
	t.Helper()

	if outcome == "success" {
		requireNoError(t, err, "second invoke: %v")

		return
	}

	code, class, ok := m.ClassifyError(err)
	if !ok || code != m.CodeModelFailure || class != m.ErrorClassRetryable {
		t.Fatalf("second outcome=%s err=%v", outcome, err)
	}
}

func isolatedInvocations(t *testing.T, invoker *Invoker, kind string) (isolatedInvocation, isolatedInvocation) {
	t.Helper()

	if kind == "identical-input" {
		request := isolationStructuredRequest()
		invoke := func(ctx context.Context) error {
			_, err := invoker.Invoke(ctx, request)

			return err
		}

		return invoke, invoke
	}

	deriver, err := structuredderive.NewDeriver(invoker)
	requireNoError(t, err, "deriver: %v")
	personal, err := m.NewTurn(m.Scope{Key: "test:shared", Kind: m.ScopeKindPersonal},
		m.ConversationRef{ConversationID: "c", AgentConversationID: "a"}, "turn", time.Now().UTC(), "private", "OK")
	requireNoError(t, err, "personal turn: %v")

	group := personal

	group.Scope.Kind = m.ScopeKindGroup
	if kind == "distinct-export" {
		group, err = m.NewTurn(group.Scope, personal.Conversation, "other-turn", personal.CompletedAt, "group data", "OK")
		requireNoError(t, err, "group turn: %v")
	}

	assertStableInvocationIdentities(t, personal, group, kind)

	return canonicalIsolatedInvocation(deriver, personal), canonicalIsolatedInvocation(deriver, group)
}

func canonicalIsolatedInvocation(deriver *structuredderive.Deriver, turn m.Turn) isolatedInvocation {
	return func(ctx context.Context) error {
		_, err := deriver.ExtractCanonicalSemantics(ctx, m.CanonicalExtractionRequest{
			SchemaVersion: m.CanonicalSchemaVersionV1, Turn: turn, Derivation: m.LegacyDerivationRef()})

		return err
	}
}

func assertStableInvocationIdentities(t *testing.T, personal, group m.Turn, kind string) {
	t.Helper()

	first, err := m.CanonicalSemanticOperationID(personal.ExportID, m.LegacyDerivationRef())
	requireNoError(t, err, "first operation: %v")
	second, err := m.CanonicalSemanticOperationID(group.ExportID, m.LegacyDerivationRef())
	requireNoError(t, err, "second operation: %v")

	same := kind == "same-export-cross-kind"
	if (personal.ExportID == group.ExportID) != same || (first == second) != same {
		t.Fatal("canonical identity fixture changed")
	}
}

func isolationStructuredRequest() structuredderive.StructuredInvocation {
	return structuredderive.StructuredInvocation{
		OperationID: "same-operation", Stage: "canonical_semantics", InputJSON: []byte(`{}`),
		OutputSchema: `{"type":"object","properties":{"output":{"type":"array"}},"required":["output"]}`}
}
