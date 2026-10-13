package badger //nolint:testpackage // Exercises supported restore of durable recovery state.

import (
	"bytes"
	"context"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestRecoveryStateSurvivesSupportedRestore(t *testing.T) {
	for _, logical := range []bool{false, true} {
		name := "backup"
		if logical {
			name = "logical"
		}

		t.Run(name, func(t *testing.T) { verifyRestoredRecovery(t, logical) })
	}
}

func verifyRestoredRecovery(t *testing.T, logical bool) {
	t.Helper()

	ctx := context.Background()
	source, err := OpenCanonicalStore(t.TempDir())
	requireEraseTestNoError(t, err)
	t.Cleanup(func() { _ = source.Close() })
	turn, record, messages := seedPreCertificateTurn(t, source)
	sourceData, err := source.LoadPayload(ctx, record.Payload)
	requireEraseTestNoError(t, err)
	_, err = source.ApplyCanonicalMutation(ctx, identityMutationForTurn(t, turn, record, messages, "certified", 0))
	requireEraseTestNoError(t, err)

	refs := []memory.PayloadRef{record.Payload}
	for _, message := range messages {
		refs = append(refs, message.Payload)
	}

	request := memory.ScopeEraseRequest{Scope: turn.Scope, OperationID: "erase-restore", Fingerprint: "restore-fp",
		ExpectedScopeVersion: 1, ForgottenAt: time.Now().UTC()}
	_, err = source.BeginScopeErase(ctx, request)
	requireEraseTestNoError(t, err)
	advanceEraseTestToStage(t, source, turn.Scope, request.OperationID, memory.ScopeEraseScrubMessages)
	restored := restoreRecoveryFixture(t, source, logical)
	pending, _, err := restored.ListPendingScopeErases(ctx, nil, 1)
	requireEraseTestNoError(t, err)

	if len(pending) != 1 || pending[0].Stage != memory.ScopeEraseScrubMessages {
		t.Fatalf("restored pending=%+v", pending)
	}

	journal, err := restored.RunScopeErase(ctx, request)
	requireEraseTestNoError(t, err)

	if journal.Stage != memory.ScopeEraseComplete || journal.Outcome.ScopeVersion != 2 {
		t.Fatalf("restored completion=%+v", journal)
	}

	requireErasePayloadsAbsent(t, restored, refs)
	proof, err := restored.VerifyCanonicalTurn(ctx, turn)
	requireEraseTestNoError(t, err)

	if !proof.Found || !proof.Forgotten {
		t.Fatalf("restored certificate=%+v", proof)
	}

	replay, err := restored.RunScopeErase(ctx, request)
	requireEraseTestNoError(t, err)

	if replay.Outcome.ScopeVersion != journal.Outcome.ScopeVersion {
		t.Fatal("restore replay duplicated logical effect")
	}

	if err := restored.PutPayload(ctx, sourceData, record.Payload); err == nil {
		t.Fatal("restored tombstone permitted raw resurrection")
	}
}

func restoreRecoveryFixture(t *testing.T, source *CanonicalStore, logical bool) *CanonicalStore {
	t.Helper()

	ctx := context.Background()

	var encoded bytes.Buffer
	if logical {
		requireEraseTestNoError(t, source.ExportCanonicalLogical(ctx, &encoded))
	} else {
		_, err := source.WriteCanonicalBackup(ctx, &encoded, 0)
		requireEraseTestNoError(t, err)
	}

	restored, err := OpenCanonicalStore(t.TempDir())
	requireEraseTestNoError(t, err)
	t.Cleanup(func() { _ = restored.Close() })

	if logical {
		requireEraseTestNoError(t, restored.ImportCanonicalLogical(ctx, &encoded))
	} else {
		requireEraseTestNoError(t, restored.RestoreCanonicalBackup(ctx, &encoded, 0))
	}

	requireEraseTestNoError(t, restored.VerifyCanonicalIntegrity(ctx))

	return restored
}
