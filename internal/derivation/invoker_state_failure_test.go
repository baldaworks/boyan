package derivation //nolint:testpackage // Exercises ID allocation, atomic ADK collisions and creator cleanup.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	m "github.com/baldaworks/boyan/pkg/memory"
	"google.golang.org/adk/v2/session"
)

type countedStateEntropy struct {
	reader io.Reader
	reads  int
}

func (r *countedStateEntropy) Read(data []byte) (int, error) {
	r.reads++

	return r.reader.Read(data)
}

type failedStateEntropy struct{}

func (failedStateEntropy) Read([]byte) (int, error) { return 0, errors.New("PRIVATE entropy failure") }

type failedCreateStateService struct{ session.Service }

func (s failedCreateStateService) Create(context.Context, *session.CreateRequest) (*session.CreateResponse, error) {
	return nil, errors.New("PRIVATE create failure")
}

func TestInvocationStatePreparationFailuresNeverDeleteOccupiedState(t *testing.T) {
	for _, kind := range []string{"entropy", "short-entropy", "create", "collision"} {
		for _, deadline := range []bool{false, true} {
			t.Run(kind+map[bool]string{false: "", true: "/deadline"}[deadline], func(t *testing.T) {
				invoker, runtime := stateFailureInvoker(t)
				occupied := "boyan-derivation-" + strings.Repeat("ab", 32)
				_, err := runtime.stateStore.service.Create(context.Background(), &session.CreateRequest{
					AppName: runtime.appName, UserID: defaultDerivationUser, SessionID: occupied,
					State: map[string]any{"owner": "preexisting"}})
				requireNoError(t, err, "seed occupied state: %v")

				reader := &countedStateEntropy{reader: bytes.NewReader(bytes.Repeat([]byte{0xab}, 32))}
				invoker.runStateEntropy = reader
				configureStatePreparationFailure(invoker, runtime, kind)

				ctx := context.Background()

				if deadline {
					var cancel context.CancelFunc

					ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
					defer cancel()
				}

				output, err := invoker.Invoke(ctx, isolationStructuredRequest())

				want := m.CodeModelFailure
				if deadline {
					want = m.CodeTimeout
				}

				assertRunnerClassification(t, string(output), err, want, m.ErrorClassRetryable)
				assertOccupiedStateUnchanged(t, runtime, occupied)

				if kind == "collision" && reader.reads != 1 {
					t.Fatalf("collision allocated %d times; want safe one-attempt failure", reader.reads)
				}
			})
		}
	}
}

func stateFailureInvoker(t *testing.T) (*Invoker, *builtRuntime) {
	t.Helper()
	invoker, err := newInvokerWithFactory(testConfig(t), &fakeFactory{}, defaultRunnerBuilder)
	requireNoError(t, err, "invoker: %v")
	t.Cleanup(func() { _ = invoker.Close(context.Background()) })

	runtime, err := invoker.ensureRuntime(context.Background())
	requireNoError(t, err, "runtime: %v")

	return invoker, runtime
}

func configureStatePreparationFailure(invoker *Invoker, runtime *builtRuntime, kind string) {
	switch kind {
	case "entropy":
		invoker.runStateEntropy = failedStateEntropy{}
	case "short-entropy":
		invoker.runStateEntropy = bytes.NewReader([]byte{1})
	case "create":
		runtime.stateStore.service = failedCreateStateService{Service: runtime.stateStore.service}
	}
}

func assertOccupiedStateUnchanged(t *testing.T, runtime *builtRuntime, id string) {
	t.Helper()

	response, err := runtime.stateStore.service.Get(context.Background(), &session.GetRequest{
		AppName: runtime.appName, UserID: defaultDerivationUser, SessionID: id})
	requireNoError(t, err, "occupied state removed: %v")

	owner, err := response.Session.State().Get("owner")
	if err != nil || owner != "preexisting" {
		t.Fatalf("occupied state overwritten: %v %v", owner, err)
	}

	listed, err := runtime.stateStore.service.List(context.Background(), &session.ListRequest{
		AppName: runtime.appName, UserID: defaultDerivationUser})
	requireNoError(t, err, "list real ADK states: %v")

	if len(listed.Sessions) != 1 {
		t.Fatalf("failed attempt leaked state: %d", len(listed.Sessions))
	}
}

func TestActualMalformedRunnerCleansOnlyOwnedState(t *testing.T) {
	factory := &schemaRejectingFactory{}
	invoker, err := newInvokerWithFactory(testConfig(t), factory, defaultRunnerBuilder)
	requireNoError(t, err, "invoker: %v")
	t.Cleanup(func() { _ = invoker.Close(context.Background()) })

	reader := &countedStateEntropy{reader: bytes.NewReader(bytes.Repeat([]byte{0xab}, 32))}
	invoker.runStateEntropy = reader
	request := isolationStructuredRequest()
	request.OutputSchema = `{"type":"object","properties":{"output":{"type":"array"}},` +
		`"required":["output"],"additionalProperties":false}`
	output, err := invoker.Invoke(context.Background(), request)
	assertRunnerClassification(t, string(output), err, m.CodeInvalidDerived, m.ErrorClassPermanent)

	id := "boyan-derivation-" + strings.Repeat("ab", 32)

	runtime := invoker.runtime
	if err := getRunState(context.Background(), runtime.stateStore, runtime.appName, id); err == nil {
		t.Fatal("malformed runner leaked owned state")
	}

	if reader.reads != 1 || factory.calls != 2 {
		t.Fatalf("allocation/provider attempts=%d/%d; want1/2", reader.reads, factory.calls)
	}
}
