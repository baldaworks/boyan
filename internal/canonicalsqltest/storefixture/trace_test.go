package storefixture_test

import (
	"context"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

type traceReader interface {
	memory.CanonicalBoundaryViewReader
	Trace(ctx context.Context, request memory.TraceRequest) (memory.TraceResponse, error)
}

func publicTraceReader(t *testing.T, store storefixture.Store) traceReader {
	t.Helper()

	r, ok := publicReader(t, store).(traceReader)
	if !ok {
		t.Fatal("public reader lacks bounded Trace and boundary views")
	}

	return r
}

func traceRequest(m memory.CanonicalMutation) memory.TraceRequest {
	return memory.TraceRequest{Scope: m.Scope, Root: memory.RevisionRef{ItemID: m.Items[0].ItemID,
		RevisionID: m.Revisions[0].RevisionID}, MaxNodes: 10}
}

func TestPublicTraceAndBoundary(t *testing.T) {
	forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, _ mutationProbe) {
		m := nativeMutation(t)
		if _, err := store.ApplyCanonicalMutation(t.Context(), m); err != nil {
			t.Fatal(err)
		}

		r := publicTraceReader(t, f.Store)

		got, err := r.Trace(t.Context(), traceRequest(m))
		if err != nil || len(got.Revisions) != 1 || len(got.Sources) != 1 {
			t.Fatalf("trace=%+v err=%v", got, err)
		}

		assertNativeTraceContent(t, got)

		if err := got.Validate(10); err != nil {
			t.Fatalf("successful Trace returned invalid DTO: %v", err)
		}

		view, err := r.LoadCanonicalBoundaryView(t.Context(), m.Scope)
		if err != nil || len(view.Snapshot.Atoms) != 1 || view.Snapshot.Atoms[0].Text != "protected memory" ||
			view.Snapshot.Version != 1 {
			t.Fatalf("view=%+v err=%v", view, err)
		}
	})
}

func assertNativeTraceContent(t *testing.T, got memory.TraceResponse) {
	t.Helper()

	if got.Revisions[0].Canonical == nil || got.Revisions[0].Canonical.Text != "protected memory" ||
		got.Sources[0].Turn == nil || len(got.Sources[0].Turn.Messages) != 2 ||
		got.Sources[0].Turn.Messages[0].Text != "remember canonical" {
		t.Fatalf("raw graph=%+v", got)
	}
}
