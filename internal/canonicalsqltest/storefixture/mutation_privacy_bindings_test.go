//go:build integration

package storefixture_test

import (
	"encoding/json"
	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"testing"
)

func TestPublicMutationPrivacyBindings(t *testing.T) {
	for _, kind := range []string{"scope", "payload", "coverage", "state", "opaque", "embedded-mismatch",
		"embedded-extra", "membership", "parent-sensitive", "reprepare-unknown"} {
		t.Run(kind, func(t *testing.T) {
			forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
				runPublicPrivacyMutation(t, nil, store, probe, kind)
			})
		})
	}
}

func runMutationRawPrivacy(t *testing.T, store mutationStore, probe mutationProbe,
	mutation memory.CanonicalMutation, kind string) {
	t.Helper()

	payload := []byte("opaque source")

	if kind != "opaque" {
		turn := newPrivacyHistory(t).turn
		if kind == "embedded-mismatch" {
			turn.Messages[0].Text = "different raw secret"
		}

		record := memory.SourceRecord{SchemaVersion: memory.DerivedSchemaVersionV1,
			Ref: memory.SourceRef{Scope: turn.Scope, ExportID: turn.ExportID,
				ConversationID: turn.Conversation.ConversationID, SourceTurnID: turn.SourceTurnID},
			State: memory.SourceStateActive, Turn: &turn}

		var err error

		payload, err = json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
	}

	mutation.Sources[0].Payload = privacyRef("raw-source", payload)

	mutation.Payloads[0] = memory.CanonicalPayload{Ref: mutation.Sources[0].Payload, Data: payload}
	if kind == "embedded-extra" {
		message := mutation.Messages[0]
		message.MessageID = "extra"
		mutation.Messages = append(mutation.Messages, message)
		mutation.Revisions[0].Evidence[0].MessageID = "extra"
	}

	_, err := store.ApplyCanonicalMutation(t.Context(), mutation)
	if err != nil {
		assertMutationCode(t, err, memory.CodeStoreFailure)
		assertMutationState(t, store, mutation.Scope, 0)

		return
	}

	if privacyState(t, probe, mutation.Scope, mutation.Sources[0].SourceID) != "unknown" {
		t.Fatal("unproven raw source authorized")
	}
}

func TestPublicMutationPrivacyReadinessReopen(t *testing.T) {
	forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, probe mutationProbe) {
		mutation := nativeMutation(t)
		if _, err := store.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
			t.Fatal(err)
		}

		probe.remove(t, canonicalsql.Key{Family: canonicalsql.SourcePrivacyReady})

		if err := f.Store.Close(); err != nil {
			t.Fatal(err)
		}

		reopened, err := f.Open(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer reopened.Close()

		store = reopened.(mutationStore)
		_, err = store.ApplyCanonicalMutation(t.Context(), mutation)
		assertMutationCode(t, err, memory.CodeUnavailable)
		_, err = store.CommitCanonicalOperation(t.Context(),
			memory.CanonicalOperationCommitRequest{Scope: mutation.Scope, OperationID: "empty", Fingerprint: "empty",
				ExpectedScopeVersion: 1, CommittedAt: mutation.Operation.CommittedAt})
		assertMutationCode(t, err, memory.CodeUnavailable)
		assertMutationState(t, store, mutation.Scope, 1)
	})
}

func runPublicPrivacyMutation(t *testing.T, _ *storefixture.Fixture, store mutationStore,
	probe mutationProbe, kind string) {
	t.Helper()
	mutation := nativeMutation(t)

	switch kind {
	case "opaque", "embedded-mismatch", "embedded-extra":
		runMutationRawPrivacy(t, store, probe, mutation, kind)

		return
	case "reprepare-unknown":
		assertUnknownMutationReprepare(t, store, probe, mutation)

		return
	}

	if _, err := store.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
		t.Fatal(err)
	}

	later := laterMutation(mutation, "later", 1)

	switch kind {
	case "membership":
		assertFreshMutationMembership(t, probe, mutation)
	case "parent-sensitive":
		assertParentMutationPrivacy(t, store, probe, mutation, later)
	default:
		assertCorruptMutationPrivacy(t, store, probe, mutation, later, kind)
	}
}

func assertCorruptMutationPrivacy(t *testing.T, store mutationStore, probe mutationProbe, mutation,
	later memory.CanonicalMutation, kind string) {
	t.Helper()

	source := mutation.Sources[0]
	record := map[string]any{"version": 1, "coverage_version": 1, "scope": mutation.Scope,
		"source_id": source.SourceID, "payload": source.Payload, "state": "standard"}

	switch kind {
	case "scope":
		record["scope"] = memory.Scope{Key: mutation.Scope.Key, Kind: memory.ScopeKindGroup}
	case "payload":
		record["payload"] = privacyRef("forged", []byte("forged"))
	case "coverage":
		record["coverage_version"] = 2
	case "state":
		record["state"] = "safe"
	}

	probe.write(t, canonicalsql.Key{Family: canonicalsql.SourcePrivacy, Scope: mutation.Scope,
		Parts: [][]byte{[]byte(source.SourceID)}}, record)
	_, err := store.ApplyCanonicalMutation(t.Context(), later)
	assertMutationCode(t, err, memory.CodeStoreFailure)
	assertMutationState(t, store, mutation.Scope, 1)
}

func assertFreshMutationMembership(t *testing.T, probe mutationProbe, mutation memory.CanonicalMutation) {
	t.Helper()

	source := mutation.Sources[0]

	var record struct {
		MessageIDs []string `json:"message_ids"`
		Complete   bool     `json:"message_membership_complete"`
	}
	probe.read(t, mutation.Scope, canonicalsql.SourcePrivacy, source.SourceID, &record)

	if len(record.MessageIDs) != 2 || !record.Complete {
		t.Fatalf("fresh membership=%+v", record)
	}
}

func assertParentMutationPrivacy(t *testing.T, store mutationStore, probe mutationProbe, mutation,
	later memory.CanonicalMutation) {
	t.Helper()

	source := mutation.Sources[0]
	later.Revisions[0].Parents = []string{"revision"}
	later.Revisions[0].Evidence = []memory.EvidenceRef{{SourceID: "missing", MessageID: "missing",
		Role: memory.MessageRoleUser, StartByte: 0, EndByte: 1, AssertionMode: memory.AssertionModeUser}}

	later.Revisions[0].Sensitivity = memory.SensitivitySensitive
	if _, err := store.ApplyCanonicalMutation(t.Context(), later); err != nil {
		t.Fatal(err)
	}

	if privacyState(t, probe, mutation.Scope, source.SourceID) != "sensitive" {
		t.Fatal("parent source not tainted")
	}
}

func assertUnknownMutationReprepare(t *testing.T, store mutationStore, probe mutationProbe,
	mutation memory.CanonicalMutation) {
	t.Helper()

	initial := mutation
	initial.Revisions = nil

	initial.Heads = nil
	if _, err := store.ApplyCanonicalMutation(t.Context(), initial); err != nil {
		t.Fatal(err)
	}

	later := laterMutation(mutation, "later", 1)
	if _, err := store.ApplyCanonicalMutation(t.Context(), later); err != nil {
		t.Fatal(err)
	}

	probe.remove(t, canonicalsql.Key{Family: canonicalsql.SourcePrivacyReady})

	if err := store.PrepareSourcePrivacy(t.Context()); err != nil {
		t.Fatal(err)
	}

	if privacyState(t, probe, mutation.Scope, mutation.Sources[0].SourceID) != "unknown" {
		t.Fatal("reprepare promoted completed unknown")
	}
}
