package canonicalcore_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestBoundaryNativeImmutableInputAfterRestart(t *testing.T) {
	runCoreAuthorities(t, func(t *testing.T, factory coreFactory) {
		for _, partial := range []bool{false, true} {
			t.Run(map[bool]string{false: "complete", true: "partial"}[partial], func(t *testing.T) {
				t.Parallel()
				f := newBoundaryReplayFixture(t, partial, factory)
				equivalent := f.export
				equivalent.TransitionID = " \t" + equivalent.TransitionID + "\n"
				equivalent.OccurredAt = equivalent.OccurredAt.In(time.FixedZone("offset", 6*60*60))

				original, err := f.processor.ProcessBoundary(context.Background(), equivalent)
				if partial {
					if code, _, ok := m.ClassifyError(err); !ok || code != m.CodeInvalidDerived {
						t.Fatalf("expected partial profile failure: %v", err)
					}
				} else {
					requireNoError(t, err, "first: %v")
				}

				f.restart(t)
				assertBoundaryNativeChangedInputs(t, f)
				f.model.invalidProfile = false
				completed, err := f.processor.ProcessBoundary(context.Background(), f.export)
				requireNoError(t, err, "original retry after restart: %v")

				if !reflect.DeepEqual(completed.Scenarios, original.Scenarios) || f.model.scenarioCalls != 1 {
					t.Fatal("retry changed completed scenario or entered scenario model")
				}

				expectedProfileCalls := 1
				if partial {
					expectedProfileCalls = 2
				}

				if f.model.profileCalls != expectedProfileCalls {
					t.Fatalf("profile calls=%d", f.model.profileCalls)
				}

				assertBoundaryNativeCompletedReplay(t, f, completed, equivalent)
			})
		}
	})
}

func assertBoundaryNativeChangedInputs(t *testing.T, f *boundaryReplayFixture) {
	t.Helper()

	ctx := context.Background()
	before, err := f.store.LoadScopeState(ctx, f.export.Scope)
	requireNoError(t, err, "before changed inputs: %v")

	scenarios, profiles := f.model.scenarioCalls, f.model.profileCalls

	changes := map[string]func(*m.Boundary){
		"reason":   func(b *m.Boundary) { b.Reason = m.BoundaryReasonReset },
		"instant":  func(b *m.Boundary) { b.OccurredAt = b.OccurredAt.Add(time.Nanosecond) },
		"agent":    func(b *m.Boundary) { b.Conversation.AgentConversationID = "different-agent" },
		"lineage":  func(b *m.Boundary) { b.Conversation.LineageID = "different-lineage" },
		"previous": func(b *m.Boundary) { b.Conversation.PreviousConversationID = "different-previous" },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			changed := f.export
			change(&changed)
			requireNoError(t, changed.Validate(), "valid changed field: %v")
			_, err = f.processor.ProcessBoundary(ctx, changed)
			requirePermanentIdentityConflict(t, err)
		})
	}

	assertBoundaryInvalidStableInputs(t, f)
	after, err := f.store.LoadScopeState(ctx, f.export.Scope)
	requireNoError(t, err, "after changed inputs: %v")

	if before != after || scenarios != f.model.scenarioCalls || profiles != f.model.profileCalls {
		t.Fatal("changed input invoked model or mutated canonical state")
	}
}

func assertBoundaryInvalidStableInputs(t *testing.T, f *boundaryReplayFixture) {
	t.Helper()

	changes := map[string]func(*m.Boundary){
		"schema":       func(b *m.Boundary) { b.SchemaVersion = "unsupported" },
		"export":       func(b *m.Boundary) { b.ExportID = "different-export" },
		"scope":        func(b *m.Boundary) { b.Scope.Key = "different-scope" },
		"conversation": func(b *m.Boundary) { b.Conversation.ConversationID = "different-conversation" },
		"transition":   func(b *m.Boundary) { b.TransitionID = "different-transition" },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			changed := f.export
			change(&changed)

			if changed.Validate() == nil {
				t.Fatal("changed stable identity unexpectedly valid")
			}

			if _, err := f.processor.ProcessBoundary(context.Background(), changed); err == nil {
				t.Fatal("invalid stable identity replayed")
			}
		})
	}
}

func assertBoundaryNativeCompletedReplay(t *testing.T, f *boundaryReplayFixture,
	expected m.BoundaryOutcome, equivalent m.Boundary) {
	t.Helper()

	ctx := context.Background()
	before, err := f.store.LoadScopeState(ctx, f.export.Scope)
	requireNoError(t, err, "before replay: %v")

	scenarios, profiles := f.model.scenarioCalls, f.model.profileCalls
	f.restart(t)
	replay, err := f.processor.ProcessBoundary(ctx, equivalent)
	requireNoError(t, err, "equivalent completed replay: %v")
	after, err := f.store.LoadScopeState(ctx, f.export.Scope)
	requireNoError(t, err, "after replay: %v")

	if !reflect.DeepEqual(expected, replay) || before != after ||
		scenarios != f.model.scenarioCalls || profiles != f.model.profileCalls {
		t.Fatal("completed equivalent replay changed outcomes/state/models")
	}
}
