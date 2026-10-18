package storefixture_test

import (
	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"testing"
)

func TestPublicTracePayloadBounds(t *testing.T) {
	for _, kind := range []string{"message", "aggregate", "source", "revision", "missing-member", "raw-corruption"} {
		t.Run(kind, func(t *testing.T) {
			forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, p mutationProbe) {
				m := nativeMutation(t)
				if _, err := store.ApplyCanonicalMutation(t.Context(), m); err != nil {
					t.Fatal(err)
				}

				alterTracePayload(t, p, m, kind)
				got, err := publicTraceReader(t, f.Store).Trace(t.Context(), traceRequest(m))
				assertEmptyTrace(t, got, err)

				if kind != "missing-member" && kind != "raw-corruption" {
					assertMutationCode(t, err, memory.CodeLimitExceeded)
				}
			})
		})
	}
}

func alterTracePayload(t *testing.T, p mutationProbe, m memory.CanonicalMutation, kind string) {
	t.Helper()

	key := func(f canonicalsql.Family, id string) canonicalsql.Key {
		return canonicalsql.Key{Family: f, Scope: m.Scope, Parts: [][]byte{[]byte(id)}}
	}

	switch kind {
	case "message", "aggregate":
		for _, message := range m.Messages {
			message.Payload.ByteSize = memory.MaxDerivedTurnTextBytes + 1
			if kind == "aggregate" {
				message.Payload.ByteSize = memory.MaxDerivedTurnTextBytes/2 + 1
			}

			p.write(t, key(canonicalsql.Message, message.MessageID), message)
		}
	case "source":
		source := m.Sources[0]
		source.Payload.ByteSize = canonicalsql.MaxEnvelopeBytes + 1
		p.write(t, key(canonicalsql.Source, source.SourceID), source)

		var privacy map[string]any
		p.read(t, m.Scope, canonicalsql.SourcePrivacy, source.SourceID, &privacy)
		privacy["payload"] = source.Payload
		p.write(t, key(canonicalsql.SourcePrivacy, source.SourceID), privacy)
	case "revision":
		revision := m.Revisions[0]
		revision.Payload.ByteSize = canonicalsql.MaxEnvelopeBytes + 1
		p.write(t, key(canonicalsql.Revision, revision.RevisionID), revision)
	case "missing-member":
		p.remove(t, key(canonicalsql.Message, m.Messages[1].MessageID))
	case "raw-corruption":
		p.write(t, canonicalsql.Key{Family: canonicalsql.Payload,
			Parts: [][]byte{[]byte(m.Messages[1].Payload.ID)}}, []byte("corrupt raw payload"))
	}
}
