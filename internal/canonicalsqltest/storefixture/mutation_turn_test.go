//go:build integration

package storefixture_test

import (
	"reflect"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func certifyMutation(t *testing.T, mutation *memory.CanonicalMutation) memory.Turn {
	t.Helper()
	turn := newPrivacyHistory(t).turn

	fingerprint, err := memory.CanonicalTurnFingerprint(turn)
	if err != nil {
		t.Fatal(err)
	}

	certificate := memory.CanonicalTurnCertificate{Version: memory.CanonicalTurnFingerprintVersion,
		SourceID: turn.SourceID, Fingerprint: fingerprint}
	for _, message := range turn.Messages {
		certificate.MessageIDs = append(certificate.MessageIDs, message.MessageID)
	}

	mutation.TurnIdentity = &memory.CanonicalTurnIdentityCommit{Turn: turn, Certificate: certificate}

	return turn
}

func TestPublicTurnMutationProofs(t *testing.T) {
	for _, kind := range []string{"new-certified", "legacy-certify", "forgotten-replay", "orphan",
		"changed-structural", "changed-input", "rollback", "retained-denial"} {
		t.Run(kind, func(t *testing.T) {
			forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, probe mutationProbe) {
				runPublicTurnMutation(t, f, store, probe, kind)
			})
		})
	}
}

func denyMutationSource(t *testing.T, probe mutationProbe, mutation memory.CanonicalMutation) {
	t.Helper()
	probe.write(t, canonicalsql.Key{Family: canonicalsql.DeniedSource, Scope: mutation.Scope,
		Parts: [][]byte{[]byte(mutation.Sources[0].SourceID)}},
		map[string]any{"source_id": mutation.Sources[0].SourceID, "denied_at": mutation.Operation.CommittedAt})
}

func runPublicTurnMutation(t *testing.T, f *storefixture.Fixture, store mutationStore, probe mutationProbe,
	kind string) {
	t.Helper()
	mutation := nativeMutation(t)

	turn := certifyMutation(t, &mutation)
	if prepareTurnMutationCase(t, store, probe, mutation, turn, kind) {
		return
	}

	outcome, err := store.ApplyCanonicalMutation(t.Context(), mutation)
	if err != nil {
		t.Fatal(err)
	}

	proof, err := f.Store.(memory.CanonicalTurnIdentityStore).VerifyCanonicalTurn(t.Context(), turn)
	if err != nil || !proof.Found || !proof.FullInputVerified {
		t.Fatalf("proof=%+v %v", proof, err)
	}

	assertTurnMutationReplay(t, store, probe, mutation, outcome, kind)
}

func assertTurnMutationReplay(t *testing.T, store mutationStore, probe mutationProbe,
	mutation memory.CanonicalMutation, outcome memory.CanonicalMutationOutcome, kind string) {
	t.Helper()

	switch kind {
	case "forgotten-replay":
		denyMutationSource(t, probe, mutation)
		probe.remove(t, canonicalsql.Key{Family: canonicalsql.Payload,
			Parts: [][]byte{[]byte(mutation.Sources[0].Payload.ID)}})

		replay, err := store.ApplyCanonicalMutation(t.Context(), mutation)
		if err != nil || !reflect.DeepEqual(replay, outcome) {
			t.Fatalf("forgotten committed replay=%+v %v", replay, err)
		}

		mutation.Operation.OperationID = "new-derivation"
		mutation.ExpectedScopeVersion = 1
		_, err = store.ApplyCanonicalMutation(t.Context(), mutation)
		assertMutationCode(t, err, memory.CodeConflict)
	case "changed-structural":
		mutation.TurnIdentity = nil

		replay, err := store.ApplyCanonicalMutation(t.Context(), mutation)
		if err != nil || !reflect.DeepEqual(replay, outcome) {
			t.Fatalf("unchanged raw certified replay=%+v %v", replay, err)
		}

		mutation.Messages[0].Role = memory.MessageRoleAssistant
		_, err = store.ApplyCanonicalMutation(t.Context(), mutation)
		assertMutationCode(t, err, memory.CodeConflict)
	case "changed-input":
		mutation.TurnIdentity.Turn.Messages[0].Text = "changed"

		_, err := store.ApplyCanonicalMutation(t.Context(), mutation)
		if err == nil {
			t.Fatal("changed authoritative turn replayed")
		}
	}
}

func prepareTurnMutationCase(t *testing.T, store mutationStore, probe mutationProbe,
	mutation memory.CanonicalMutation, turn memory.Turn, kind string) bool {
	t.Helper()

	switch kind {
	case "legacy-certify", "orphan", "retained-denial":
		legacy := mutation

		legacy.TurnIdentity = nil
		if _, err := store.ApplyCanonicalMutation(t.Context(), legacy); err != nil {
			t.Fatal(err)
		}
	}

	if kind == "orphan" {
		probe.remove(t, canonicalsql.Key{Family: canonicalsql.Source, Scope: mutation.Scope,
			Parts: [][]byte{[]byte(turn.SourceID)}})
		_, err := store.ApplyCanonicalMutation(t.Context(), mutation)
		assertMutationCode(t, err, memory.CodeConflict)

		return true
	}

	if kind == "retained-denial" {
		denyMutationSource(t, probe, mutation)
		_, err := store.ApplyCanonicalMutation(t.Context(), mutation)
		assertMutationCode(t, err, memory.CodeConflict)

		return true
	}

	if kind == "rollback" {
		before := probe.snapshot(t)
		release := probe.failInserts(t, "operation")
		_, err := store.ApplyCanonicalMutation(t.Context(), mutation)

		release()

		if err == nil || !reflect.DeepEqual(before, probe.snapshot(t)) {
			t.Fatal("failed mutation published turn proof")
		}
	}

	return false
}
