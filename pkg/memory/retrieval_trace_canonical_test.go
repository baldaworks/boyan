package memory_test

import (
	"encoding/json"
	m "github.com/baldaworks/boyan/pkg/memory"
	"testing"
	"time"
)

func TestCanonicalTraceUnionValidation(t *testing.T) {
	t.Parallel()

	revision, valid := canonicalTraceFixture(t)

	hit := m.SearchHit{Canonical: &revision}
	if _, err := hit.Validate(); err == nil {
		t.Fatal("derived search accepted canonical trace identity")
	}

	raw := valid.Sources[0]
	scope := valid.Scope

	cases := []struct {
		name   string
		change func(*m.SearchHit)
	}{
		{"empty text", func(h *m.SearchHit) { h.Canonical.Text = "" }},
		{"invalid temporal", func(h *m.SearchHit) { h.Canonical.Temporal.ObservedAt = time.Time{} }},
		{"mixed", func(h *m.SearchHit) { h.Atom = &m.Atom{} }},
		{"score", func(h *m.SearchHit) { score := 1.0; h.Score = &score }},
		{"invalid identity", func(h *m.SearchHit) { h.Canonical.RevisionID = "" }},
		{"invalidated", func(h *m.SearchHit) { h.Canonical.State = m.RevisionStateInvalidated }},
		{"supersedes without parent",
			func(h *m.SearchHit) {
				h.Canonical.Supersedes = &m.RevisionRef{ItemID: "item",
					RevisionID: "old"}
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			copyOf := revision
			invalid := m.SearchHit{Canonical: &copyOf}
			tc.change(&invalid)

			response := m.TraceResponse{SchemaVersion: m.DerivedSchemaVersionV1,
				Trust: m.ReferenceTrustUntrusted,
				Scope: scope,
				Root: m.RevisionRef{ItemID: "item",
					RevisionID: "revision"},
				Revisions: []m.SearchHit{invalid},
				Sources:   []m.SourceRecord{raw}}
			if err := response.Validate(10); err == nil {
				t.Fatal("invalid trace accepted")
			}
		})
	}
}

func TestMixedCanonicalAndLegacyTraceClosure(t *testing.T) {
	t.Parallel()

	response, atom := mixedTraceFixture(t)
	if err := response.Validate(10); err != nil {
		t.Fatalf("mixed trace: %v", err)
	}

	atom.Meta.RevisionID = "forged"

	if err := response.Validate(10); err == nil {
		t.Fatal("legacy identity validation weakened")
	}
}

func canonicalTraceFixture(t *testing.T) (m.CanonicalTraceRevision, m.TraceResponse) {
	t.Helper()

	scope := m.Scope{Key: "test:trace", Kind: m.ScopeKindPersonal}
	revision := m.CanonicalTraceRevision{SchemaVersion: m.CanonicalSchemaVersionV1,
		Sensitivity: m.SensitivityStandard, Retention: m.RetentionClassStandard,
		Scope:      scope,
		ItemID:     "item",
		RevisionID: "revision",
		Revision:   1,
		Kind:       m.MemoryKindState,
		State:      m.RevisionStateActive,
		Text:       "memory",
		CreatedAt:  time.Now().UTC()}
	revision.Temporal = m.Temporal{ObservedAt: revision.CreatedAt}
	source := m.SourceRef{Scope: scope, ExportID: "export", ConversationID: "conversation", SourceTurnID: "turn"}
	revision.Provenance = m.Provenance{RawSources: []m.SourceRef{source}}

	turn,
		err := m.NewTurn(scope,
		m.ConversationRef{ConversationID: "conversation",
			AgentConversationID: "agent"},
		"turn",
		revision.CreatedAt,
		"memory",
		"OK")
	if err != nil {
		t.Fatalf("turn: %v", err)
	}

	source.ExportID = turn.ExportID
	revision.Provenance.RawSources = []m.SourceRef{source}
	raw := m.SourceRecord{SchemaVersion: m.DerivedSchemaVersionV1, Ref: source, State: m.SourceStateActive, Turn: &turn}

	valid := m.TraceResponse{SchemaVersion: m.DerivedSchemaVersionV1,
		Trust: m.ReferenceTrustUntrusted,
		Scope: scope,
		Root: m.RevisionRef{ItemID: "item",
			RevisionID: "revision"},
		Revisions: []m.SearchHit{{Canonical: &revision}},
		Sources:   []m.SourceRecord{raw}}
	if err := valid.Validate(10); err != nil {
		t.Fatalf("valid trace: %v", err)
	}

	return revision, valid
}

func TestCanonicalTraceGraphBounds(t *testing.T) {
	t.Parallel()
	revision, valid := canonicalTraceFixture(t)
	duplicate := valid

	duplicate.Revisions = append(append([]m.SearchHit(nil), valid.Revisions...), valid.Revisions[0])
	if err := duplicate.Validate(10); err == nil {
		t.Fatal("duplicate graph accepted")
	}

	if err := valid.Validate(1); err == nil {
		t.Fatal("graph node bound ignored")
	}

	cycle := revision
	cycle.Provenance = m.Provenance{ParentRevisions: []m.RevisionRef{valid.Root}}
	cyclic := valid
	cyclic.Revisions = []m.SearchHit{{Canonical: &cycle}}

	cyclic.Sources = nil
	if err := cyclic.Validate(10); err == nil {
		t.Fatal("cycle accepted")
	}
}

func mixedTraceFixture(t *testing.T) (m.TraceResponse, *m.Atom) {
	t.Helper()

	scope := derivedTestScope()
	atom := derivedTestAtom(t, scope)
	parent := m.RevisionRef{ItemID: atom.Meta.ItemID, RevisionID: atom.Meta.RevisionID}
	native := mixedTraceNative(scope, atom, parent)

	turn,
		err := m.NewTurn(scope,
		m.ConversationRef{ConversationID: "conversation-1",
			AgentConversationID: "agent"},
		"turn-1",
		derivedTestTime(),
		"memory",
		"OK")
	if err != nil {
		t.Fatalf("turn: %v", err)
	}

	atom.Meta.Provenance.RawSources[0].ExportID = turn.ExportID

	atom.Meta.RevisionID,
		err = m.DerivedRevisionID(scope,
		atom.Meta.ItemID,
		atom.Meta.OperationID,
		[]string{string(atom.Category),
			atom.Text,
			string(atom.Relation)},
		atom.Meta.Provenance,
		nil)
	if err != nil {
		t.Fatalf("revision: %v", err)
	}

	native.Provenance.ParentRevisions[0].RevisionID = atom.Meta.RevisionID
	source := m.SourceRecord{SchemaVersion: m.DerivedSchemaVersionV1,
		Ref:   atom.Meta.Provenance.RawSources[0],
		State: m.SourceStateActive,
		Turn:  &turn}

	response := m.TraceResponse{SchemaVersion: m.DerivedSchemaVersionV1,
		Trust: m.ReferenceTrustUntrusted,
		Scope: scope,
		Root: m.RevisionRef{ItemID: native.ItemID,
			RevisionID: native.RevisionID},
		Revisions: []m.SearchHit{{Canonical: &native},
			{Atom: &atom}},
		Sources: []m.SourceRecord{source}}

	return response, &atom
}

func TestCanonicalTraceRequiresVisibilityMetadata(t *testing.T) {
	t.Parallel()

	revision, valid := canonicalTraceFixture(t)
	for _, tc := range []struct{ name, sensitivity, retention string }{
		{"missing sensitivity", "", "standard"},
		{"sensitive", "sensitive", "standard"},
		{"invalid sensitivity", "unknown", "standard"},
		{"missing retention", "standard", ""},
		{"invalid retention", "standard", "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content, err := json.Marshal(revision)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var metadata map[string]any
			if err = json.Unmarshal(content, &metadata); err != nil {
				t.Fatalf("metadata: %v", err)
			}

			metadata["sensitivity"] = tc.sensitivity
			metadata["retention"] = tc.retention

			content, err = json.Marshal(metadata)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var decoded m.CanonicalTraceRevision
			if err = json.Unmarshal(content, &decoded); err != nil {
				t.Fatalf("decode: %v", err)
			}

			response := valid

			response.Revisions = []m.SearchHit{{Canonical: &decoded}}
			if err = response.Validate(10); err == nil {
				t.Fatal("trace visibility metadata ignored")
			}
		})
	}
}

func mixedTraceNative(scope m.Scope, atom m.Atom, parent m.RevisionRef) m.CanonicalTraceRevision {
	native := m.CanonicalTraceRevision{SchemaVersion: m.CanonicalSchemaVersionV1,
		Sensitivity: m.SensitivityStandard, Retention: m.RetentionClassStandard,
		Scope:      scope,
		ItemID:     "native",
		RevisionID: "native-revision",
		Revision:   1,
		Kind:       m.MemoryKindState,
		State:      m.RevisionStateActive,
		Text:       "native memory",
		CreatedAt:  atom.Meta.CreatedAt,
		Temporal:   m.Temporal{ObservedAt: atom.Meta.CreatedAt},
		Provenance: m.Provenance{ParentRevisions: []m.RevisionRef{parent}}}

	return native
}
