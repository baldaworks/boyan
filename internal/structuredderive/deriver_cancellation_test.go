package structuredderive

import (
	"context"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

type lateSuccessInvoker struct {
	cancel context.CancelFunc
}

func (i lateSuccessInvoker) Invoke(context.Context, StructuredInvocation) ([]byte, error) {
	i.cancel()

	return []byte(`{"output":[]}`), nil
}

func (lateSuccessInvoker) Close(context.Context) error { return nil }

func TestAdapterRejectsSuccessAfterContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	deriver := &Deriver{
		invoker: lateSuccessInvoker{cancel: cancel}, maxInputBytes: 1024, maxOutputBytes: 1024,
	}
	var output struct {
		Output []any `json:"output"`
	}
	err := deriver.invoke(ctx, StructuredInvocation{
		OperationID: "late-success", Stage: "canonical_semantics", InputJSON: []byte(`{}`),
	}, &output)

	code, class, ok := memory.ClassifyError(err)
	if !ok || code != memory.CodeModelFailure || class != memory.ErrorClassRetryable {
		t.Fatalf("classification = %s/%s/%t, want model_failure/retryable/true", code, class, ok)
	}
}
