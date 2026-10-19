package storefixture_test

import (
	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"testing"
	"time"
)

func TestPublicReaderPrivacyAndCorruption(t *testing.T) {
	for _, kind := range []string{"source-denied", "revision-denied", "sensitive", "revision-sensitive", "unknown",
		"incomplete", "unprepared", "scope-fence", "payload-missing", "payload-corrupt", "head-corrupt", "source-binding",
		"membership-subset", "message-owner", "certified-owner", "scope-stamp", "restore-loading"} {
		t.Run(kind, func(t *testing.T) {
			forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, probe mutationProbe) {
				mutation := nativeMutation(t)
				if kind == "membership-subset" || kind == "certified-owner" {
					certifyMutation(t, &mutation)
				}

				if _, err := store.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
					t.Fatal(err)
				}

				want := alterReaderAuthority(t, probe, mutation, kind)
				reader := publicReader(t, f.Store)

				records, err := reader.LoadRecallRecords(t.Context(), mutation.Scope, []string{"revision"})
				if len(records) != 0 {
					t.Fatalf("unauthorized content=%+v", records)
				}

				if want != "" {
					assertMutationCode(t, err, want)
				} else if err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}

func alterReaderAuthority(t *testing.T, p mutationProbe, m memory.CanonicalMutation, kind string) memory.ErrorCode {
	t.Helper()

	scope := m.Scope
	key := func(f canonicalsql.Family, id string) canonicalsql.Key {
		return canonicalsql.Key{Family: f, Scope: scope, Parts: [][]byte{[]byte(id)}}
	}

	switch kind {
	case "source-denied":
		denyMutationSource(t, p, m)
	case "revision-denied":
		p.write(t, key(canonicalsql.DeniedRevision, "revision"), map[string]any{"revision_id": "revision",
			"denied_at": time.Now()})
	case "sensitive":
		source := m.Sources[0]
		source.Sensitivity = memory.SensitivitySensitive
		p.write(t, key(canonicalsql.Source, source.SourceID), source)
	case "revision-sensitive":
		revision := m.Revisions[0]
		revision.Sensitivity = memory.SensitivitySensitive
		p.write(t, key(canonicalsql.Revision, revision.RevisionID), revision)
	case "unknown", "membership-subset", "source-binding":
		return alterReaderPrivacyRecord(t, p, m, kind)
	case "incomplete":
		p.write(t, key(canonicalsql.SourcePrivacyScope, "coverage"), map[string]any{"version": 1, "scope": scope,
			"incomplete": true})
	case "unprepared":
		p.remove(t, canonicalsql.Key{Family: canonicalsql.SourcePrivacyReady})

		return memory.CodeUnavailable
	case "scope-fence":
		installMutationFence(t, p, scope)

		return memory.CodeConflict
	default:
		return alterReaderStoredData(t, p, m, kind)
	}

	return ""
}

func alterReaderStoredData(t *testing.T, p mutationProbe, m memory.CanonicalMutation, kind string) memory.ErrorCode {
	t.Helper()

	key := func(f canonicalsql.Family, id string) canonicalsql.Key {
		return canonicalsql.Key{Family: f, Scope: m.Scope, Parts: [][]byte{[]byte(id)}}
	}

	switch kind {
	case "payload-missing":
		p.remove(t, canonicalsql.Key{Family: canonicalsql.Payload, Parts: [][]byte{[]byte(m.Revisions[0].Payload.ID)}})
	case "payload-corrupt":
		p.write(t, canonicalsql.Key{Family: canonicalsql.Payload,
			Parts: [][]byte{[]byte(m.Revisions[0].Payload.ID)}}, []byte("corrupt"))

		return memory.CodeStoreFailure
	case "head-corrupt":
		p.write(t, key(canonicalsql.Head, "item"), memory.ItemHead{ItemID: "other", RevisionID: "revision"})

		return memory.CodeStoreFailure
	case "message-owner", "certified-owner":
		message := m.Messages[0]
		message.SourceID = "other"
		p.write(t, key(canonicalsql.Message, message.MessageID), message)

		return memory.CodeStoreFailure
	case "scope-stamp":
		if err := p.exec("UPDATE "+p.table("row_stamp")+" SET last_seq=$1 WHERE family=$2",
			canonicalsql.Uint64(100), []byte(canonicalsql.ScopeState)); err != nil {
			t.Fatal(err)
		}

		return memory.CodeStoreFailure
	case "restore-loading":
		if err := p.exec("INSERT INTO "+p.table("restore_control")+" (singleton,control) VALUES (1,$1)",
			[]byte("loading")); err != nil {
			t.Fatal(err)
		}

		return memory.CodeUnavailable
	}

	return ""
}

func alterReaderPrivacyRecord(t *testing.T, p mutationProbe, m memory.CanonicalMutation, kind string) memory.ErrorCode {
	t.Helper()

	var record map[string]any
	p.read(t, m.Scope, canonicalsql.SourcePrivacy, m.Sources[0].SourceID, &record)

	switch kind {
	case "membership-subset":
		record["message_ids"] = []string{m.Messages[0].MessageID}
		record["message_membership_complete"] = true
	case "source-binding":
		record["source_id"] = "other"
	default:
		record["state"] = kind
	}

	p.write(t, canonicalsql.Key{Family: canonicalsql.SourcePrivacy, Scope: m.Scope,
		Parts: [][]byte{[]byte(m.Sources[0].SourceID)}}, record)

	if kind == "membership-subset" || kind == "source-binding" {
		return memory.CodeStoreFailure
	}

	return ""
}
