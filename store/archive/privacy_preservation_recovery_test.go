package archive_test

import (
	"bytes"
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

type privacyPreservationRecoveryStore interface {
	memory.ScopeEraseStore
	VerifyCanonicalTurn(ctx context.Context, turn memory.Turn) (memory.CanonicalTurnIdentityResult, error)
	LoadPayload(ctx context.Context, ref memory.PayloadRef) ([]byte, error)
	PutPayload(ctx context.Context, data []byte, ref memory.PayloadRef) error
}

func privacyPreservationPendingErase(t *testing.T, source *storefixture.Fixture) (
	memory.CanonicalMutation, memory.ScopeEraseRequest, memory.ScopeEraseJournal, []byte,
) {
	t.Helper()
	m, _ := privacyPreservationSeed(t, source)
	request := memory.ScopeEraseRequest{Scope: m.Scope, OperationID: "pending-restore", Fingerprint: "pending-restore",
		ExpectedScopeVersion: 1, ForgottenAt: m.Operation.CommittedAt.Add(time.Hour)}

	original, err := source.Store.(memory.ScopeEraseStore).BeginScopeErase(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}

	if original.Stage != memory.ScopeErasePrepared || original.SourceCount != 0 || original.RevisionCount != 0 {
		t.Fatalf("invalid pending baseline: %+v", original)
	}

	return m, request, original, logicalBytes(t, source.Store.(logicalStore))
}

func TestSQLPrivacyPreservationEraseRecoveryPreflight(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, source *storefixture.Fixture) {
		_, request, original, full := privacyPreservationPendingErase(t, source)
		d := restoreDestination(t, source.Backend)
		remove := installRestoreFault(t, newArchiveProbe(t, d), "privacy")
		err := d.Store.(logicalStore).ImportCanonicalLogical(t.Context(), bytes.NewReader(full))

		remove()

		if err == nil {
			t.Fatal("validated preparation fault ignored")
		}

		privacyPreservationNoSpool(t)
		privacyPreservationReopen(t, d)

		if err := d.Store.(restoreStore).ResumeRestoreBeforeEraseRecovery(t.Context()); err != nil {
			t.Fatal("validated preflight: ", err)
		}

		privacyPreservationOriginalPending(t, d, original)
		err = d.Store.(logicalStore).ImportCanonicalLogical(t.Context(), bytes.NewReader(full))
		privacyPreservationRequireCode(t, err, memory.CodeConflict)
		privacyPreservationNoSpool(t)

		p := newArchiveProbe(t, d)
		if err := p.c.Update(t.Context(), func(tx *canonicalsql.Tx) error {
			return tx.Delete(t.Context(), canonicalsql.Key{Family: canonicalsql.SourcePrivacyReady})
		}); err != nil {
			t.Fatal(err)
		}

		if err := d.Store.(restoreStore).ResumeRestoreBeforeEraseRecovery(t.Context()); err != nil {
			t.Fatal("missing readiness blocked narrow preflight: ", err)
		}

		eraser := d.Store.(memory.ScopeEraseStore)

		pending, _, err := eraser.ListPendingScopeErases(t.Context(), nil, 1)
		if err != nil || len(pending) != 1 || !reflect.DeepEqual(pending[0], original) {
			t.Fatalf("pending after ready removal=%+v error=%v", pending, err)
		}

		privacyPreservationFinishPending(t, eraser, request)

		if err := d.Store.(restoreStore).PrepareSourcePrivacy(t.Context()); err != nil {
			t.Fatal("post-recovery preparation: ", err)
		}
	})
}

func TestSQLPrivacyPreservationLoadingEraseWholeRetry(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, source *storefixture.Fixture) {
		m, request, original, full := privacyPreservationPendingErase(t, source)

		control := restoreDestination(t, source.Backend)
		if err := control.Store.(logicalStore).ImportCanonicalLogical(t.Context(), bytes.NewReader(full)); err != nil {
			t.Fatal("valid complete pending source control: ", err)
		}

		privacyPreservationOriginalPending(t, control, original)

		lines := bytes.Split(bytes.TrimSuffix(full, []byte{'\n'}), []byte{'\n'})
		prefix := append(bytes.Join(lines[:len(lines)-1], []byte{'\n'}), '\n')

		d := restoreDestination(t, source.Backend)
		if err := d.Store.(logicalStore).ImportCanonicalLogical(t.Context(), bytes.NewReader(prefix)); err == nil {
			t.Fatal("footerless pending archive accepted")
		}

		privacyPreservationNoSpool(t)
		privacyPreservationLoadingEraseFences(t, d, m, request)
		privacyPreservationReopen(t, d)

		if err := d.Store.(logicalStore).ImportCanonicalLogical(t.Context(), bytes.NewReader(prefix)); err == nil {
			t.Fatal("same malformed retry accepted")
		}

		privacyPreservationNoSpool(t)
		privacyPreservationLoadingEraseFences(t, d, m, request)

		if err := d.Store.(logicalStore).ImportCanonicalLogical(t.Context(), bytes.NewReader(full)); err != nil {
			t.Fatal("whole valid pending-erase retry: ", err)
		}

		privacyPreservationOriginalPending(t, d, original)
		privacyPreservationFinishPending(t, d.Store.(memory.ScopeEraseStore), request)
	})
}

func privacyPreservationRequireCode(t *testing.T, err error, want memory.ErrorCode) {
	t.Helper()

	code, _, ok := memory.ClassifyError(err)
	if !ok || code != want {
		t.Fatalf("error=%v want=%s", err, want)
	}
}

func privacyPreservationOriginalPending(t *testing.T, f *storefixture.Fixture, original memory.ScopeEraseJournal) {
	t.Helper()

	got, found, err := f.Store.(memory.ScopeEraseStore).LoadScopeErase(t.Context(), original.Scope, original.OperationID)
	if err != nil || !found || !reflect.DeepEqual(got, original) {
		t.Fatalf("pending journal advanced before recovery: %+v error=%v", got, err)
	}
}

func privacyPreservationLoadingEraseFences(t *testing.T, f *storefixture.Fixture,
	m memory.CanonicalMutation, request memory.ScopeEraseRequest) {
	t.Helper()
	privacyPreservationUnavailable(t, f, m)
	_, err := f.Store.(memory.ScopeEraseStore).AdvanceScopeErase(t.Context(), request.Scope, request.OperationID)
	privacyPreservationRequireCode(t, err, memory.CodeUnavailable)
	_, err = f.Store.(memory.CanonicalOperationCommitter).CommitCanonicalOperation(t.Context(),
		memory.CanonicalOperationCommitRequest{
			Scope: request.Scope, OperationID: "blocked-writer", Fingerprint: "blocked-writer", CommittedAt: request.ForgottenAt,
		})
	privacyPreservationRequireCode(t, err, memory.CodeUnavailable)
	privacyPreservationRequireCode(t, f.Store.(restoreStore).ResumeRestoreBeforeEraseRecovery(t.Context()),
		memory.CodeUnavailable)
}

func privacyPreservationFinishPending(t *testing.T, eraser memory.ScopeEraseStore, request memory.ScopeEraseRequest) {
	t.Helper()

	got, err := eraser.RunScopeErase(t.Context(), request)
	if err != nil || got.Stage != memory.ScopeEraseComplete || got.SourceCount != 1 || got.RevisionCount != 1 ||
		got.Outcome == nil || got.Outcome.ScopeVersion != 2 {
		t.Fatalf("recovered pending erase=%+v error=%v", got, err)
	}
}

func TestSQLPrivacyPreservationRecoveryScrubMessages(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, source *storefixture.Fixture) {
		m, turn, request := privacyPreservationScrubSource(t, source)

		for _, logical := range []bool{false, true} {
			route := "native"
			if logical {
				route = "logical"
			}

			t.Run(route, func(t *testing.T) {
				privacyPreservationRestoredScrub(t, source, m, turn, request, logical)
			})
		}
	})
}

func privacyPreservationScrubSource(t *testing.T, source *storefixture.Fixture) (
	memory.CanonicalMutation, memory.Turn, memory.ScopeEraseRequest,
) {
	t.Helper()

	s := source.Store.(privacyPreservationStore)
	if err := s.PrepareSourcePrivacy(t.Context()); err != nil {
		t.Fatal(err)
	}

	m, turn := privacyPreservationMutation(t, memory.Scope{Key: "test:archive-recovery",
		Kind: memory.ScopeKindPersonal}, "first")

	fingerprint, err := memory.CanonicalTurnFingerprint(turn)
	if err != nil {
		t.Fatal(err)
	}

	certificate := memory.CanonicalTurnCertificate{Version: memory.CanonicalTurnFingerprintVersion,
		SourceID: turn.SourceID, Fingerprint: fingerprint}
	for _, message := range turn.Messages {
		certificate.MessageIDs = append(certificate.MessageIDs, message.MessageID)
	}

	m.TurnIdentity = &memory.CanonicalTurnIdentityCommit{Turn: turn, Certificate: certificate}
	if _, err := s.ApplyCanonicalMutation(t.Context(), m); err != nil {
		t.Fatal(err)
	}

	privacyPreservationTrace(t, source, m, "")

	request := memory.ScopeEraseRequest{Scope: m.Scope, OperationID: "erase-restore",
		Fingerprint: "restore-fp", ExpectedScopeVersion: 1,
		ForgottenAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}
	eraser := source.Store.(memory.ScopeEraseStore)

	journal, err := eraser.BeginScopeErase(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}

	for steps := 0; journal.Stage != memory.ScopeEraseScrubMessages; steps++ {
		if steps > 32 {
			t.Fatalf("scrub stage not reached: %+v", journal)
		}

		journal, err = eraser.AdvanceScopeErase(t.Context(), request.Scope, request.OperationID)
		if err != nil {
			t.Fatal(err)
		}
	}

	return m, turn, request
}

func privacyPreservationRestoredScrub(
	t *testing.T, source *storefixture.Fixture, m memory.CanonicalMutation,
	turn memory.Turn, request memory.ScopeEraseRequest, logical bool,
) {
	t.Helper()

	_, data := privacyPreservationArtifact(t, source, logical)

	d := restoreDestination(t, source.Backend)
	if err := restoreJournalArtifact(t, d.Store.(restoreStore), data, logical); err != nil {
		t.Fatal(err)
	}

	restored := d.Store.(privacyPreservationRecoveryStore)

	pending, _, err := restored.ListPendingScopeErases(t.Context(), nil, 1)
	if err != nil || len(pending) != 1 || pending[0].Stage != memory.ScopeEraseScrubMessages {
		t.Fatalf("pending=%+v error=%v", pending, err)
	}

	completed, err := restored.RunScopeErase(t.Context(), request)
	if err != nil || completed.Stage != memory.ScopeEraseComplete || completed.Outcome == nil ||
		completed.Outcome.ScopeVersion != 2 {
		t.Fatalf("completion=%+v error=%v", completed, err)
	}

	privacyPreservationScrubbedPayloads(t, restored, m)
	privacyPreservationScrubbedProofReplay(t, restored, turn, request, completed)
}

func privacyPreservationScrubbedPayloads(
	t *testing.T, restored privacyPreservationRecoveryStore, m memory.CanonicalMutation,
) {
	t.Helper()

	for _, payload := range m.Payloads {
		if _, err := restored.LoadPayload(t.Context(), payload.Ref); err == nil {
			t.Fatal("scrubbed payload survived")
		}

		if err := restored.PutPayload(t.Context(), payload.Data, payload.Ref); err == nil {
			t.Fatal("restored tombstone allowed resurrection")
		}
	}
}

func privacyPreservationScrubbedProofReplay(
	t *testing.T, restored privacyPreservationRecoveryStore, turn memory.Turn,
	request memory.ScopeEraseRequest, completed memory.ScopeEraseJournal,
) {
	t.Helper()

	proof, err := restored.VerifyCanonicalTurn(t.Context(), turn)
	if err != nil || !proof.Found || !proof.Forgotten {
		t.Fatalf("forgotten proof=%+v error=%v", proof, err)
	}

	replay, err := restored.RunScopeErase(t.Context(), request)
	if err != nil || !reflect.DeepEqual(replay.Outcome, completed.Outcome) {
		t.Fatalf("erase replay=%+v error=%v want=%+v", replay.Outcome, err, completed.Outcome)
	}
}
