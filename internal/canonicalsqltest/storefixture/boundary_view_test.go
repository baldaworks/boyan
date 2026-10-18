package storefixture_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

type boundaryAuxReader interface {
	LoadActiveRecallRecords(ctx context.Context, scope memory.Scope, limit uint32) ([]memory.RecallRecord, error)
	LoadCanonicalCompatibility(ctx context.Context, scope memory.Scope,
		id string) (memory.CanonicalCompatibilityPayload, bool, error)
}

func TestPublicBoundaryCompatibilityAndTemporal(t *testing.T) {
	for _, kind := range []memory.DerivedKind{memory.DerivedKindAtom, memory.DerivedKindScenario,
		memory.DerivedKindProfile} {
		t.Run(string(kind), func(t *testing.T) {
			forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, _ mutationProbe) {
				runBoundaryCompatibility(t, f, store, kind)
			})
		})
	}
}

func runBoundaryCompatibility(t *testing.T, f *storefixture.Fixture, store mutationStore, kind memory.DerivedKind) {
	t.Helper()
	m := compatibleTraceMutation(t)
	from, until := m.Operation.CommittedAt.Add(time.Nanosecond), m.Operation.CommittedAt.Add(3*time.Nanosecond)
	m.Revisions[0].Temporal.ValidFrom, m.Revisions[0].Temporal.ExpiresAt = &from, &until

	m.Revisions[0].Retention = memory.RetentionClassEphemeral
	if _, err := store.ApplyCanonicalMutation(t.Context(), m); err != nil {
		t.Fatal(err)
	}

	root := traceRequest(m)
	if kind != memory.DerivedKindAtom {
		child := compatibleBoundaryChild(t, m, kind)
		if _, err := store.ApplyCanonicalMutation(t.Context(), child); err != nil {
			t.Fatal(err)
		}

		root.Root = memory.RevisionRef{ItemID: child.Items[0].ItemID, RevisionID: child.Revisions[0].RevisionID}
	}

	r := publicTraceReader(t, f.Store)

	trace, err := r.Trace(t.Context(), root)
	if err != nil || len(trace.Sources) != 1 {
		t.Fatalf("compatible trace=%+v err=%v", trace, err)
	}

	assertCompatibilityTraceShape(t, trace, kind)

	assertBoundaryTemporal(t, r, m, kind, from, until)

	aux, ok := r.(boundaryAuxReader)
	if !ok {
		t.Fatal("boundary auxiliary ports missing")
	}

	compat, found, err := aux.LoadCanonicalCompatibility(t.Context(), m.Scope, m.Revisions[0].RevisionID)
	if err != nil || !found || compat.Text != "legacy memory" {
		t.Fatalf("compat=%+v found=%v err=%v", compat, found, err)
	}

	_, err = r.LoadCanonicalBoundaryViewAt(t.Context(), m.Scope, time.Time{})
	assertMutationCode(t, err, memory.CodeInvalidDerived)
}

func compatibleTraceMutation(t *testing.T) memory.CanonicalMutation {
	t.Helper()
	m := nativeMutation(t)
	turn := newPrivacyHistory(t).turn
	ref := memory.SourceRef{Scope: m.Scope, ExportID: turn.ExportID,
		ConversationID: turn.Conversation.ConversationID, SourceTurnID: turn.SourceTurnID}
	category := memory.AtomCategoryFact

	itemID, err := memory.AtomItemID(m.Scope, category, "legacy memory")
	if err != nil {
		t.Fatal(err)
	}

	revisionID, err := memory.DerivedRevisionID(m.Scope, itemID, "legacy-op",
		[]string{string(category), "legacy memory", string(memory.CandidateRelationNew)},
		memory.Provenance{RawSources: []memory.SourceRef{ref}}, nil)
	if err != nil {
		t.Fatal(err)
	}

	compat := memory.CanonicalCompatibilityPayload{SchemaVersion: memory.CanonicalCompatibilitySchemaVersion,
		Kind: memory.DerivedKindAtom, Category: &category, Text: "legacy memory", LegacyItemID: itemID,
		LegacyRevisionID: revisionID, LegacyOperationID: "legacy-op"}
	installBoundaryCompatibility(t, &m, compat)

	return m
}

func compatibleBoundaryChild(t *testing.T, parent memory.CanonicalMutation,
	kind memory.DerivedKind) memory.CanonicalMutation {
	t.Helper()

	m := traceLaterMutation(parent)
	provenance := memory.Provenance{ParentRevisions: []memory.RevisionRef{{ItemID: parent.Items[0].ItemID,
		RevisionID: parent.Revisions[0].RevisionID}}}
	itemID, err := memory.ProfileItemID(m.Scope)
	parts := []string{"derived memory"}

	if kind == memory.DerivedKindScenario {
		itemID, err = memory.ScenarioItemID(m.Scope, "topic")
		parts = []string{"topic", "Title", "derived memory"}
	}

	if err != nil {
		t.Fatal(err)
	}

	revisionID, err := memory.DerivedRevisionID(m.Scope, itemID, "child-op", parts, provenance, nil)
	if err != nil {
		t.Fatal(err)
	}

	compat := memory.CanonicalCompatibilityPayload{SchemaVersion: memory.CanonicalCompatibilitySchemaVersion,
		Kind: kind, Text: "derived memory", LegacyItemID: itemID, LegacyRevisionID: revisionID,
		LegacyOperationID: "child-op", LegacyParents: provenance.ParentRevisions}
	if kind == memory.DerivedKindScenario {
		compat.TopicKey, compat.Title = "topic", "Title"
	}

	m.Items = append([]memory.MemoryItem(nil), parent.Items...)
	m.Revisions[0].Revision = 1
	m.Revisions[0].Temporal.ExpiresAt, m.Revisions[0].Temporal.ValidFrom = nil, nil
	m.Revisions[0].Retention = memory.RetentionClassStandard
	installBoundaryCompatibility(t, &m, compat)

	return m
}

func installBoundaryCompatibility(t *testing.T, m *memory.CanonicalMutation,
	compat memory.CanonicalCompatibilityPayload) {
	t.Helper()

	data, err := json.Marshal(compat)
	if err != nil {
		t.Fatal(err)
	}

	m.Items[0].ItemID = compat.LegacyItemID
	m.Items[0].MemoryKey = memory.MemoryKey("derived:" + string(compat.Kind))
	m.Revisions[0].ItemID, m.Revisions[0].RevisionID = compat.LegacyItemID, compat.LegacyRevisionID
	m.Revisions[0].Payload = privacyRef("compat:"+string(compat.Kind), data)
	m.Payloads = append(m.Payloads, memory.CanonicalPayload{Ref: m.Revisions[0].Payload, Data: data})
	m.Heads = []memory.ItemHead{{ItemID: compat.LegacyItemID, RevisionID: compat.LegacyRevisionID}}
}

func assertBoundaryTemporal(t *testing.T, r traceReader, m memory.CanonicalMutation,
	kind memory.DerivedKind, from, until time.Time) {
	t.Helper()

	for _, at := range []time.Time{m.Operation.CommittedAt, from, until.Add(-time.Nanosecond), until} {
		view, err := r.LoadCanonicalBoundaryViewAt(t.Context(), m.Scope, at)
		if err != nil {
			t.Fatal(err)
		}

		want := 1
		if kind != memory.DerivedKindAtom {
			want = 2
		}

		if len(view.LegacyToCanonical) != want || len(view.Boundary.Parents) != want {
			t.Fatalf("structural=%+v", view)
		}

		eligible := len(view.Boundary.Eligible.Atoms) + len(view.Boundary.Eligible.Scenarios) +
			len(view.Boundary.Eligible.Profiles)

		if at.Before(from) || !at.Before(until) {
			want = 0
		}

		if eligible != want {
			t.Fatalf("at=%v eligible=%d want=%d", at, eligible, want)
		}

		assertBoundaryInheritedParents(t, view, until)
	}
}

func assertCompatibilityTraceShape(t *testing.T, trace memory.TraceResponse, kind memory.DerivedKind) {
	t.Helper()

	if kind == memory.DerivedKindAtom && trace.Revisions[0].Atom == nil {
		t.Fatal("legacy atom shape lost")
	}

	if kind == memory.DerivedKindScenario && trace.Revisions[0].Scenario == nil {
		t.Fatal("legacy scenario shape lost")
	}

	if kind == memory.DerivedKindProfile && trace.Revisions[0].Profile == nil {
		t.Fatal("legacy profile shape lost")
	}
}

func assertBoundaryInheritedParents(t *testing.T, view memory.CanonicalBoundaryView, until time.Time) {
	t.Helper()

	for _, parent := range view.Boundary.Parents {
		if parent.Temporal.ExpiresAt == nil || !parent.Temporal.ExpiresAt.Equal(until) ||
			parent.Retention != memory.RetentionClassEphemeral {
			t.Fatalf("inherited temporal metadata=%+v", parent)
		}
	}
}
