package archive_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// No archive is created here: this isolates revision recall from raw-source
// disclosure before any restore or metadata reconstruction can influence it.
func TestSQLPrivacyPreservationBenignRecallBeforeRestore(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		m, _ := privacyPreservationSeed(t, f)
		privacyPreservationBenignRecall(t, f, m)
		privacyPreservationLateSensitive(t, f, m)
	})
}

func TestSQLPrivacyPreservationRoundTrips(t *testing.T) {
	for _, kind := range []string{"native-standard", "migrated-embedded-only", "mixed-same",
		"mixed-separate", "opaque-source", "incomplete-evidence", "missing", "forged-standard"} {
		t.Run(kind, func(t *testing.T) {
			storefixture.ForEach(t, func(t *testing.T, source *storefixture.Fixture) {
				m, turn := privacyPreservationMutation(t, memory.Scope{Key: "test:archive-privacy",
					Kind: memory.ScopeKindPersonal}, "first")

				s := source.Store.(privacyPreservationStore)
				if err := s.PrepareSourcePrivacy(t.Context()); err != nil {
					t.Fatal(err)
				}

				privacyPreservationConfigure(t, &m, turn, kind)

				if _, err := s.ApplyCanonicalMutation(t.Context(), m); err != nil {
					t.Fatal(err)
				}

				privacyPreservationHistorical(t, source, m, kind)

				for _, logical := range []bool{false, true} {
					route := "native"
					if logical {
						route = "logical"
					}

					t.Run(route, func(t *testing.T) {
						privacyPreservationRoundtrip(t, source, m, logical, kind)
					})
				}
			})
		})
	}
}

func privacyPreservationConfigure(t *testing.T, m *memory.CanonicalMutation, turn memory.Turn, kind string) {
	t.Helper()

	switch kind {
	case "migrated-embedded-only":
		privacyPreservationEmbed(t, m, turn)
	case "mixed-same", "mixed-separate", "forged-standard":
		sensitive := m.Revisions[0]
		sensitive.RevisionID = "sensitive-history"
		sensitive.Sensitivity = memory.SensitivitySensitive
		sensitive.Evidence = append([]memory.EvidenceRef(nil), sensitive.Evidence...)

		if kind == "mixed-separate" {
			m.Messages[1].Role = memory.MessageRoleUser
			sensitive.Evidence[0] = memory.EvidenceRef{SourceID: m.Sources[0].SourceID, MessageID: m.Messages[1].MessageID,
				Role: memory.MessageRoleUser, EndByte: m.Messages[1].Payload.ByteSize,
				TextDigest: m.Messages[1].Payload.Digest, AssertionMode: memory.AssertionModeUser}
		}

		m.Revisions = append(m.Revisions, sensitive)
	case "opaque-source":
		m.Sources[0].Payload = privacyPreservationRef("opaque", []byte("opaque"))
		m.Payloads[0] = memory.CanonicalPayload{Ref: m.Sources[0].Payload, Data: []byte("opaque")}
	case "incomplete-evidence":
		m.Revisions[0].Evidence[0].TextDigest = ""
	}
}

func privacyPreservationEmbed(t *testing.T, m *memory.CanonicalMutation, turn memory.Turn) {
	t.Helper()

	embedded := memory.SourceRecord{SchemaVersion: memory.DerivedSchemaVersionV1,
		Ref: memory.SourceRef{Scope: turn.Scope, ExportID: turn.ExportID,
			ConversationID: turn.Conversation.ConversationID, SourceTurnID: turn.SourceTurnID},
		State: memory.SourceStateActive, Turn: &turn}

	data, err := json.Marshal(embedded)
	if err != nil {
		t.Fatal(err)
	}

	m.Sources[0].Payload = privacyPreservationRef("embedded", data)
	m.Payloads[0] = memory.CanonicalPayload{Ref: m.Sources[0].Payload, Data: data}
}

func privacyPreservationHistorical(t *testing.T, f *storefixture.Fixture, m memory.CanonicalMutation, kind string) {
	t.Helper()
	p := newArchiveProbe(t, f)

	key := privacyPreservationKey(canonicalsql.SourcePrivacy, m, m.Sources[0].SourceID)
	if kind == "forged-standard" {
		value := privacyPreservationRead(t, p, key)
		value["state"], value["sticky_unknown"] = "standard", false
		privacyPreservationWrite(t, p, key, value)
	}

	if kind != "missing" && kind != "migrated-embedded-only" {
		return
	}

	if err := p.c.Update(t.Context(), func(tx *canonicalsql.Tx) error {
		for _, k := range []canonicalsql.Key{key, {Family: canonicalsql.SourcePrivacyReady},
			privacyPreservationKey(canonicalsql.SourcePrivacyScope, m, "coverage")} {
			if err := tx.Delete(t.Context(), k); err != nil {
				return err
			}
		}

		if kind == "migrated-embedded-only" {
			for _, message := range m.Messages {
				if err := tx.Delete(t.Context(), privacyPreservationKey(canonicalsql.Message, m, message.MessageID)); err != nil {
					return err
				}
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func privacyPreservationBenignRecall(t *testing.T, f *storefixture.Fixture, m memory.CanonicalMutation) {
	t.Helper()

	rows, err := privacyPreservationPublicReader(t, f).LoadRecallRecords(t.Context(), m.Scope,
		[]string{m.Revisions[0].RevisionID})
	if err != nil || len(rows) != 1 || rows[0].Text != "protected memory" {
		t.Fatalf("benign recall=%+v error=%v", rows, err)
	}
}

func privacyPreservationLateSensitive(t *testing.T, f *storefixture.Fixture, m memory.CanonicalMutation) {
	t.Helper()

	later := m
	later.ExpectedScopeVersion = 1
	later.Operation.OperationID, later.Operation.Fingerprint = "late-sensitive", "late-sensitive"
	later.Sources, later.Messages, later.Payloads = nil, nil, nil

	later.Items = append([]memory.MemoryItem(nil), m.Items...)
	later.Items[0].ItemID = "late-sensitive-item"

	later.Revisions = append([]memory.MemoryRevision(nil), m.Revisions...)
	later.Revisions[0].RevisionID, later.Revisions[0].ItemID = "late-sensitive-revision", later.Items[0].ItemID
	later.Revisions[0].Sensitivity = memory.SensitivitySensitive
	later.Heads = []memory.ItemHead{{ItemID: later.Items[0].ItemID, RevisionID: later.Revisions[0].RevisionID}}
	p := newArchiveProbe(t, f)
	remove := privacyPreservationOperationFault(t, p)
	_, err := f.Store.(privacyPreservationStore).ApplyCanonicalMutation(t.Context(), later)

	remove()

	if err == nil {
		t.Fatal("operation-insert failure committed")
	}

	key := privacyPreservationKey(canonicalsql.SourcePrivacy, m, m.Sources[0].SourceID)
	if value := privacyPreservationRead(t, p, key); value["state"] != "standard" {
		t.Fatal("aborted sensitivity escaped: ", value)
	}

	privacyPreservationTrace(t, f, m, "")

	if _, err := f.Store.(privacyPreservationStore).ApplyCanonicalMutation(t.Context(), later); err != nil {
		t.Fatal(err)
	}

	privacyPreservationTrace(t, f, m, memory.CodeForgotten)
	privacyPreservationBenignRecall(t, f, m)
}

func privacyPreservationOperationFault(t *testing.T, p archiveProbe) func() {
	t.Helper()

	if p.table("operation") == "operation" {
		if err := p.exec("CREATE TRIGGER task19_privacy_operation BEFORE INSERT ON operation " +
			"BEGIN SELECT RAISE(ABORT,'task19 fault'); END"); err != nil {
			t.Fatal(err)
		}

		return func() {
			if err := p.exec("DROP TRIGGER task19_privacy_operation"); err != nil {
				t.Fatal(err)
			}
		}
	}

	if err := p.exec("CREATE FUNCTION boyan_canonical.task19_privacy_operation() RETURNS trigger " +
		"LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'task19 fault'; END $$"); err != nil {
		t.Fatal(err)
	}

	if err := p.exec("CREATE TRIGGER task19_privacy_operation BEFORE INSERT ON boyan_canonical.operation " +
		"FOR EACH ROW EXECUTE FUNCTION boyan_canonical.task19_privacy_operation()"); err != nil {
		t.Fatal(err)
	}

	return func() {
		if err := p.exec("DROP TRIGGER task19_privacy_operation ON boyan_canonical.operation"); err != nil {
			t.Fatal(err)
		}

		if err := p.exec("DROP FUNCTION boyan_canonical.task19_privacy_operation()"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSQLPrivacyPreservationFullIncrementalSources(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, source *storefixture.Fixture) {
		first, _ := privacyPreservationSeed(t, source)
		base, _, full := backupFrames(t, source.Store.(backupStore), 0)

		d := restoreDestination(t, source.Backend)
		if err := restoreJournalArtifact(t, d.Store.(restoreStore), full, false); err != nil {
			t.Fatal(err)
		}

		next, _ := privacyPreservationMutation(t, first.Scope, "second")

		next.ExpectedScopeVersion = 1
		if _, err := source.Store.(privacyPreservationStore).ApplyCanonicalMutation(t.Context(), next); err != nil {
			t.Fatal(err)
		}

		_, _, delta := backupFrames(t, source.Store.(backupStore), base)
		if err := d.Store.(restoreStore).RestoreCanonicalBackup(t.Context(), bytes.NewReader(delta), 1); err != nil {
			t.Fatal(err)
		}

		p := newArchiveProbe(t, d)
		for _, m := range []memory.CanonicalMutation{first, next} {
			value := privacyPreservationRead(t, p, privacyPreservationKey(canonicalsql.SourcePrivacy, m, m.Sources[0].SourceID))
			if value["state"] != "standard" {
				t.Fatal("incremental classification: ", value)
			}

			privacyPreservationTrace(t, d, m, "")
		}

		state, err := d.Store.(privacyPreservationStore).LoadScopeState(t.Context(), first.Scope)
		if err != nil || state.Version != 2 {
			t.Fatalf("scope cursor=%+v error=%v", state, err)
		}
	})
}

func TestSQLPrivacyPreservationRestrictiveTraceAndScopeIsolation(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, source *storefixture.Fixture) {
		m, _ := privacyPreservationSeed(t, source)

		_, _, full := backupFrames(t, source.Store.(backupStore), 0)
		for _, restriction := range []string{"sensitive", "unknown", "scope"} {
			t.Run(restriction, func(t *testing.T) {
				d := restoreDestination(t, source.Backend)
				if err := restoreJournalArtifact(t, d.Store.(restoreStore), full, false); err != nil {
					t.Fatal(err)
				}

				privacyPreservationRestrict(t, d, m, restriction)

				if err := restoreJournalArtifact(t, d.Store.(restoreStore), full, false); err != nil {
					t.Fatal(err)
				}

				privacyPreservationTrace(t, d, m, memory.CodeForgotten)

				if restriction == "scope" {
					privacyPreservationScopeIsolation(t, d, m)
				}
			})
		}
	})
}

func TestSQLPrivacyPreservationEmbeddedSubsetImport(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		s := f.Store.(privacyPreservationStore)
		if err := s.PrepareSourcePrivacy(t.Context()); err != nil {
			t.Fatal(err)
		}

		m, turn := privacyPreservationMutation(t, memory.Scope{Key: "test:archive-embedded",
			Kind: memory.ScopeKindPersonal}, "embedded")
		privacyPreservationEmbed(t, &m, turn)

		if _, err := s.ApplyCanonicalMutation(t.Context(), m); err != nil {
			t.Fatal(err)
		}

		privacyPreservationTrace(t, f, m, "")
		frames, good := privacyPreservationArtifact(t, f, false)

		d := restoreDestination(t, f.Backend)
		if err := restoreJournalArtifact(t, d.Store.(restoreStore), good, false); err != nil {
			t.Fatal("valid embedded control: ", err)
		}

		frame := privacyPreservationFrame(t, frames, canonicalsql.SourcePrivacy)
		value := privacyPreservationFrameValue(t, frame)
		value["message_ids"], value["message_membership_complete"] = []string{m.Messages[0].MessageID}, true
		privacyPreservationSetFrame(t, frame, canonicalsql.SourcePrivacy, value)

		if err := restoreJournalArtifact(t, d.Store.(restoreStore), privacyPreservationEncode(t, frames, false),
			false); err == nil {
			t.Fatal("retained full capture healed invalid completed embedded subset")
		}

		privacyPreservationUnavailable(t, d, m)
	})
}

func privacyPreservationRoundtrip(
	t *testing.T, source *storefixture.Fixture, m memory.CanonicalMutation, logical bool, kind string,
) {
	t.Helper()

	_, data := privacyPreservationArtifact(t, source, logical)

	d := restoreDestination(t, source.Backend)
	if err := restoreJournalArtifact(t, d.Store.(restoreStore), data, logical); err != nil {
		t.Fatal(err)
	}

	want := memory.CodeForgotten
	if kind == "native-standard" || kind == "migrated-embedded-only" || kind == "missing" {
		want = ""
	}

	privacyPreservationTrace(t, d, m, want)

	if kind == "mixed-same" || kind == "mixed-separate" || kind == "forged-standard" {
		privacyPreservationBenignRecall(t, d, m)
	}

	if kind == "native-standard" {
		privacyPreservationStandardRoundtrip(t, d, m, logical, data)
	}
}

func privacyPreservationStandardRoundtrip(
	t *testing.T, d *storefixture.Fixture, m memory.CanonicalMutation, logical bool, data []byte,
) {
	t.Helper()

	if logical {
		if err := restoreJournalArtifact(t, d.Store.(restoreStore), data, true); err != nil {
			t.Fatal("logical retry: ", err)
		}
	}

	privacyPreservationLateSensitive(t, d, m)
}

func privacyPreservationScopeIsolation(t *testing.T, d *storefixture.Fixture, m memory.CanonicalMutation) {
	t.Helper()

	p := newArchiveProbe(t, d)

	personal := privacyPreservationRead(t, p, privacyPreservationKey(canonicalsql.SourcePrivacyScope, m, "coverage"))
	if personal["incomplete"] != true {
		t.Fatal("personal restriction lost")
	}

	group := m.Scope
	group.Kind = memory.ScopeKindGroup

	if err := p.c.View(t.Context(), func(tx *canonicalsql.Tx) error {
		_, err := tx.Get(t.Context(), canonicalsql.Key{Family: canonicalsql.SourcePrivacyScope,
			Scope: group, Parts: [][]byte{[]byte("coverage")}})

		code, _, ok := memory.ClassifyError(err)
		if !ok || code != memory.CodeNotFound {
			t.Fatalf("scope-kind taint crossed lane: %v", err)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
