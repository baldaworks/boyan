package derivation //nolint:testpackage // Exercises the private runner collector and real structured wrapper.

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/structuredderive"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/normahq/runtime/v2/agentfactory"
	"github.com/normahq/runtime/v2/structuredagent"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func TestCollectRunnerOutputClassifiesStructuredValidation(t *testing.T) {
	deadline, cancelDeadline := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelDeadline()

	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	sentinel := structuredagent.ErrStructuredOutputSchemaValidation
	private := errors.New("PRIVATE provider response")
	wrapped := fmt.Errorf("wrapper: %w", &structuredagent.OutputValidationError{
		Err: errors.Join(sentinel, private), AccumulatedOutput: "PRIVATE malformed output"})

	tests := []struct {
		name  string
		ctx   context.Context
		err   error
		code  memory.ErrorCode
		class memory.ErrorClass
	}{
		{name: "output sentinel", ctx: context.Background(), err: sentinel,
			code: memory.CodeInvalidDerived, class: memory.ErrorClassPermanent},
		{name: "wrapped joined validation", ctx: context.Background(), err: wrapped,
			code: memory.CodeInvalidDerived, class: memory.ErrorClassPermanent},
		{name: "provider", ctx: context.Background(), err: private,
			code: memory.CodeModelFailure, class: memory.ErrorClassRetryable},
		{name: "input sentinel", ctx: context.Background(), err: structuredagent.ErrStructuredInputSchemaValidation,
			code: memory.CodeModelFailure, class: memory.ErrorClassRetryable},
		{name: "validation context without sentinel", ctx: context.Background(),
			err:  &structuredagent.OutputValidationError{Err: private, AccumulatedOutput: "PRIVATE"},
			code: memory.CodeModelFailure, class: memory.ErrorClassRetryable},
		{name: "deadline precedence", ctx: deadline, err: wrapped,
			code: memory.CodeTimeout, class: memory.ErrorClassRetryable},
		{name: "canceled provider", ctx: canceled, err: context.Canceled,
			code: memory.CodeModelFailure, class: memory.ErrorClassRetryable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			events := func(yield func(*runEvent, error) bool) { yield(nil, test.err) }
			output, err := collectRunnerOutput(test.ctx, events)
			assertRunnerClassification(t, output, err, test.code, test.class)
		})
	}
}

func assertRunnerClassification(
	t *testing.T, output string, err error, code memory.ErrorCode, class memory.ErrorClass,
) {
	t.Helper()

	actualCode, actualClass, ok := memory.ClassifyError(err)
	if !ok || actualCode != code || actualClass != class {
		t.Fatalf("classification=%s/%s error=%v, want %s/%s", actualCode, actualClass, err, code, class)
	}

	if output != "" || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatalf("runner error disclosed provider output: %q %v", output, err)
	}
}

type schemaRejectingFactory struct {
	fakeFactory

	calls int
}

func (f *schemaRejectingFactory) Build(_ context.Context, request agentfactory.BuildRequest) (adkagent.Agent, error) {
	return adkagent.New(adkagent.Config{Name: request.Name, Description: request.Description,
		Run: func(ctx adkagent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				f.calls++
				event := session.NewEvent(context.Background(), ctx.InvocationID())
				event.Content = genai.NewContentFromText(`{"private":"PRIVATE malformed response"}`, genai.RoleModel)
				event.TurnComplete = true
				yield(event, nil)
			}
		}})
}

func TestInvokeClassifiesActualStructuredWrapperValidation(t *testing.T) {
	factory := &schemaRejectingFactory{}
	invoker, err := newInvokerWithFactory(testConfig(t), factory, defaultRunnerBuilder)
	requireNoError(t, err, "invoker: %v")
	t.Cleanup(func() { _ = invoker.Close(context.Background()) })

	output, err := invoker.Invoke(context.Background(), structuredderive.StructuredInvocation{
		OperationID: "runtime-schema-rejection", Stage: "canonical_semantics", InputJSON: []byte(`{}`),
		OutputSchema: `{"type":"object","properties":{"output":{"type":"array"}},` +
			`"required":["output"],"additionalProperties":false}`})
	assertRunnerClassification(t, string(output), err, memory.CodeInvalidDerived, memory.ErrorClassPermanent)

	if factory.calls != 2 {
		t.Fatalf("actual structured wrapper calls=%d, want configured initial attempt and retry", factory.calls)
	}
}
