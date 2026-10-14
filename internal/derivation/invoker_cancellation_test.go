package derivation //nolint:testpackage // Uses private runtime seams to force initialization and runner races.

import (
	"context"
	"iter"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/structuredderive"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/normahq/runtime/v2/agentfactory"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

type blockingBuildFactory struct {
	fakeFactory

	started chan struct{}
	release chan struct{}
}

func (f *blockingBuildFactory) Build(_ context.Context, request agentfactory.BuildRequest) (adkagent.Agent, error) {
	select {
	case <-f.started:
	default:
		close(f.started)
	}

	<-f.release

	return adkagent.New(adkagent.Config{Name: request.Name, Description: request.Description})
}

func TestCanceledRuntimeWaiterReturnsPromptly(t *testing.T) {
	factory := &blockingBuildFactory{started: make(chan struct{}), release: make(chan struct{})}
	invoker, err := newInvokerWithFactory(testConfig(t), factory, defaultRunnerBuilder)
	requireNoError(t, err, "new invoker: %v")
	t.Cleanup(func() { _ = invoker.Close(context.Background()) })

	firstDone := make(chan error, 1)

	go func() {
		_, buildErr := invoker.ensureRuntime(context.Background())
		firstDone <- buildErr
	}()

	<-factory.started

	waiterCtx, cancel := context.WithCancel(context.Background())
	waiterDone := make(chan error, 1)

	go func() {
		_, waitErr := invoker.ensureRuntime(waiterCtx)
		waiterDone <- waitErr
	}()

	cancel()

	select {
	case waitErr := <-waiterDone:
		assertRunnerClassification(t, "", waitErr, memory.CodeModelFailure, memory.ErrorClassRetryable)
	case <-time.After(time.Second):
		close(factory.release)
		<-firstDone
		t.Fatal("canceled runtime waiter remained blocked behind provider initialization")
	}

	close(factory.release)

	if buildErr := <-firstDone; buildErr != nil {
		t.Fatalf("initial build: %v", buildErr)
	}
}

func TestCanceledInvocationRejectsLateProviderOutput(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	invoker, err := newInvokerWithFactory(testConfig(t), &fakeFactory{}, func(
		adkagent.Agent, runStateStore, string,
	) (eventRunner, error) {
		return fakeRunner{run: func(
			context.Context, string, string, *genai.Content, adkagent.RunConfig,
		) iter.Seq2[*runEvent, error] {
			return func(yield func(*runEvent, error) bool) {
				close(started)
				<-release
				yield(&runEvent{LLMResponse: model.LLMResponse{Content: &genai.Content{
					Parts: []*genai.Part{{Text: `{"output":[]}`}},
				}}}, nil)
			}
		}}, nil
	})
	requireNoError(t, err, "new invoker: %v")
	t.Cleanup(func() { _ = invoker.Close(context.Background()) })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		_, invokeErr := invoker.Invoke(ctx, structuredderive.StructuredInvocation{
			OperationID: "late-output", Stage: "canonical_semantics", InputJSON: []byte(`{}`),
			OutputSchema: `{"type":"object"}`,
		})
		done <- invokeErr
	}()

	<-started
	cancel()
	close(release)

	assertRunnerClassification(t, "", <-done, memory.CodeModelFailure, memory.ErrorClassRetryable)
}
