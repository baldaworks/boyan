package storefixture_test

import (
	"bytes"
	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"testing"
)

func TestPublicTraceManifestAndRawEnvelope(t *testing.T) {
	for _, kind := range []string{"missing", "empty", "duplicate", "reversed", "source-envelope",
		"message-envelope", "copied-certificate", "foreign-parent"} {
		t.Run(kind, func(t *testing.T) {
			forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, p mutationProbe) {
				m := nativeMutation(t)
				if kind == "copied-certificate" {
					certifyMutation(t, &m)
				}

				if _, err := store.ApplyCanonicalMutation(t.Context(), m); err != nil {
					t.Fatal(err)
				}

				alterTraceManifest(t, p, m, kind)

				got, err := publicTraceReader(t, f.Store).Trace(t.Context(), traceRequest(m))
				if kind == "reversed" {
					if err != nil || len(got.Sources) != 1 || got.Sources[0].Turn.Messages[0].Role != memory.MessageRoleUser {
						t.Fatalf("source message ordering changed: %+v %v", got, err)
					}
				} else {
					assertEmptyTrace(t, got, err)
				}
			})
		})
	}
}

func alterTraceManifest(t *testing.T, p mutationProbe, m memory.CanonicalMutation, kind string) {
	t.Helper()

	key := canonicalsql.Key{Family: canonicalsql.SourcePrivacy, Scope: m.Scope,
		Parts: [][]byte{[]byte(m.Sources[0].SourceID)}}
	if kind == "missing" {
		p.remove(t, key)

		return
	}

	var privacy map[string]any
	p.read(t, m.Scope, canonicalsql.SourcePrivacy, m.Sources[0].SourceID, &privacy)

	switch kind {
	case "empty":
		privacy["message_ids"] = []string{}
	case "duplicate":
		privacy["message_ids"] = []string{m.Messages[0].MessageID, m.Messages[0].MessageID}
	case "reversed":
		privacy["message_ids"] = []string{m.Messages[1].MessageID, m.Messages[0].MessageID}
	default:
		alterTraceRawEnvelope(t, p, m, kind)

		return
	}

	p.write(t, key, privacy)
}

func alterTraceRawEnvelope(t *testing.T, p mutationProbe, m memory.CanonicalMutation, kind string) {
	t.Helper()

	switch kind {
	case "source-envelope", "message-envelope":
		family, id := canonicalsql.Source, m.Sources[0].SourceID
		data := []byte("corrupt canonical source")

		if kind == "message-envelope" {
			family, id = canonicalsql.Message, m.Messages[0].MessageID

			data = bytes.Repeat([]byte("x"), canonicalsql.MaxEnvelopeBytes+1)
			if err := p.exec("UPDATE "+p.table(string(family))+" SET envelope=$1 WHERE id=$2", data, []byte(id)); err == nil {
				t.Fatal("SQL accepted an oversized envelope")
			}

			data = data[:canonicalsql.MaxEnvelopeBytes]
		}

		if err := p.exec("UPDATE "+p.table(string(family))+" SET envelope=$1 WHERE id=$2", data, []byte(id)); err != nil {
			t.Fatal(err)
		}
	case "copied-certificate":
		certificate := m.TurnIdentity.Certificate
		certificate.SourceID = "foreign-source"
		p.write(t, canonicalsql.Key{Family: canonicalsql.TurnCertificate, Scope: m.Scope,
			Parts: [][]byte{[]byte(m.Sources[0].SourceID)}}, certificate)
	case "foreign-parent":
		parent := m.Revisions[0]
		parent.RevisionID = "foreign-parent"
		foreign := m.Scope
		foreign.Kind = memory.ScopeKindGroup
		p.write(t, canonicalsql.Key{Family: canonicalsql.Revision, Scope: foreign,
			Parts: [][]byte{[]byte(parent.RevisionID)}}, parent)

		child := m.Revisions[0]
		child.Parents = []string{parent.RevisionID}
		p.write(t, canonicalsql.Key{Family: canonicalsql.Revision, Scope: m.Scope,
			Parts: [][]byte{[]byte(child.RevisionID)}}, child)
	}
}
