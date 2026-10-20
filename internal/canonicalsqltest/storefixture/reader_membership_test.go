//go:build integration

package storefixture_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestPublicReaderIncompleteMembershipAuthority(t *testing.T) {
	for _, kind := range []string{"uncertified-omitted-owner", "uncertified-incomplete", "certified-omitted-owner",
		"valid-certified", "valid-embedded-only", "embedded-omitted-owner", "evidence-digest",
		"second-evidence-omitted", "certified-extra-evidence", "embedded-evidence-digest"} {
		t.Run(kind, func(t *testing.T) {
			forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, p mutationProbe) {
				m := nativeMutation(t)
				if strings.Contains(kind, "certified") && !strings.HasPrefix(kind, "uncertified") {
					certifyMutation(t, &m)
				}

				if _, err := store.ApplyCanonicalMutation(t.Context(), m); err != nil {
					t.Fatal(err)
				}

				alterReaderMembership(t, p, m, kind)
				records, err := publicReader(t, f.Store).LoadRecallRecords(t.Context(), m.Scope, []string{"revision"})
				t.Logf("case=%s returned=%d error=%v", kind, len(records), err)

				if strings.HasPrefix(kind, "valid-") {
					if err != nil || len(records) != 1 || records[0].Text != "protected memory" {
						t.Fatalf("valid independent membership unavailable: records=%+v error=%v", records, err)
					}
				} else if len(records) != 0 {
					t.Fatalf("content authorized without bound evidence/membership: %q", records[0].Text)
				}
			})
		})
	}
}

func alterReaderMembership(t *testing.T, p mutationProbe, m memory.CanonicalMutation, kind string) {
	t.Helper()

	key := func(f canonicalsql.Family, id string) canonicalsql.Key {
		return canonicalsql.Key{Family: f, Scope: m.Scope, Parts: [][]byte{[]byte(id)}}
	}

	var privacy map[string]any
	p.read(t, m.Scope, canonicalsql.SourcePrivacy, m.Sources[0].SourceID, &privacy)

	if privacy["state"] != "standard" {
		t.Fatalf("fixture classification=%v", privacy["state"])
	}

	privacy["message_membership_complete"] = false

	privacy["message_ids"] = []string{}
	if strings.Contains(kind, "embedded") {
		installReaderEmbeddedSource(t, p, &m, privacy, kind == "valid-embedded-only")
	}

	if kind == "embedded-evidence-digest" {
		alterReaderEmbeddedEvidence(t, p, m)
	}

	if strings.HasSuffix(kind, "omitted-owner") {
		message := m.Messages[0]
		message.SourceID = "foreign-owner"
		p.write(t, key(canonicalsql.Message, message.MessageID), message)
	}

	if kind == "evidence-digest" || kind == "second-evidence-omitted" || kind == "certified-extra-evidence" {
		alterReaderEvidence(t, p, m, privacy, kind)
	}

	p.write(t, key(canonicalsql.SourcePrivacy, m.Sources[0].SourceID), privacy)
}

func alterReaderEmbeddedEvidence(t *testing.T, p mutationProbe, m memory.CanonicalMutation) {
	t.Helper()

	message := m.Messages[0]
	data := []byte("different raw text")
	message.Payload = privacyRef("different-message", data)
	p.write(t, canonicalsql.Key{Family: canonicalsql.Payload, Parts: [][]byte{[]byte(message.Payload.ID)}}, data)
	p.write(t, canonicalsql.Key{Family: canonicalsql.Message, Scope: m.Scope,
		Parts: [][]byte{[]byte(message.MessageID)}}, message)
	revision := m.Revisions[0]
	revision.Evidence[0].TextDigest = message.Payload.Digest
	revision.Evidence[0].EndByte = message.Payload.ByteSize
	p.write(t, canonicalsql.Key{Family: canonicalsql.Revision, Scope: m.Scope,
		Parts: [][]byte{[]byte(revision.RevisionID)}}, revision)
}

func installReaderEmbeddedSource(t *testing.T, p mutationProbe, m *memory.CanonicalMutation,
	privacy map[string]any, only bool) {
	t.Helper()
	turn := newPrivacyHistory(t).turn
	embedded := memory.SourceRecord{SchemaVersion: memory.DerivedSchemaVersionV1,
		Ref: memory.SourceRef{Scope: turn.Scope, ExportID: turn.ExportID,
			ConversationID: turn.Conversation.ConversationID, SourceTurnID: turn.SourceTurnID},
		State: memory.SourceStateActive, Turn: &turn}

	data, err := json.Marshal(embedded)
	if err != nil {
		t.Fatal(err)
	}

	source := &m.Sources[0]
	source.Payload = privacyRef("reader-embedded", data)
	p.write(t, canonicalsql.Key{Family: canonicalsql.Payload, Parts: [][]byte{[]byte(source.Payload.ID)}}, data)
	p.write(t, canonicalsql.Key{Family: canonicalsql.Source, Scope: m.Scope,
		Parts: [][]byte{[]byte(source.SourceID)}}, source)
	privacy["payload"] = source.Payload

	if only {
		for _, message := range m.Messages {
			p.remove(t, canonicalsql.Key{Family: canonicalsql.Message, Scope: m.Scope,
				Parts: [][]byte{[]byte(message.MessageID)}})
		}
	}
}

func alterReaderEvidence(t *testing.T, p mutationProbe, m memory.CanonicalMutation, privacy map[string]any,
	kind string) {
	t.Helper()

	revision := m.Revisions[0]
	privacy["message_membership_complete"] = true
	privacy["message_ids"] = []string{m.Messages[0].MessageID, m.Messages[1].MessageID}

	if kind == "evidence-digest" {
		revision.Evidence[0].TextDigest = strings.Repeat("0", 64)
	} else {
		message := m.Messages[0]
		message.MessageID = "uncaptured-evidence"
		p.write(t, canonicalsql.Key{Family: canonicalsql.Message, Scope: m.Scope,
			Parts: [][]byte{[]byte(message.MessageID)}}, message)

		ref := revision.Evidence[0]
		ref.MessageID = message.MessageID
		revision.Evidence = append(revision.Evidence, ref)

		if kind == "certified-extra-evidence" {
			privacy["message_membership_complete"] = false
			privacy["message_ids"] = []string{}
		}
	}

	p.write(t, canonicalsql.Key{Family: canonicalsql.Revision, Scope: m.Scope,
		Parts: [][]byte{[]byte(revision.RevisionID)}}, revision)
}
