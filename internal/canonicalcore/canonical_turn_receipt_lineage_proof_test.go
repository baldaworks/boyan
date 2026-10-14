package canonicalcore_test

import (
	"context"
	"net/http"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestTurnReceiptMissingCertificateCannotProveStrippedLineage(t *testing.T) {
	f := newTurnReceiptFixture(t)
	f.turn.Conversation.LineageID = "original-lineage"

	f.turn.Conversation.PreviousConversationID = "previous-conversation"
	if response := f.post(t, f.turn); response.Code != http.StatusOK {
		t.Fatalf("initial lineage input rejected: %d %s", response.Code, response.Body)
	}

	deleteTurnReceiptRecord(t, f, "turn-certificate", f.turn.SourceID)
	retry := f.turn
	retry.Conversation.LineageID = ""
	retry.Conversation.PreviousConversationID = ""

	response := f.post(t, retry)
	if response.Code != http.StatusConflict || f.calls != 1 || f.processCalls != 1 {
		t.Fatalf("unproven stripped lineage replayed: status=%d body=%s extractor=%d processor=%d",
			response.Code, response.Body, f.calls, f.processCalls)
	}

	assertMissingLineageProofRejectsSuppliedFingerprint(t, f, retry)
}

func assertMissingLineageProofRejectsSuppliedFingerprint(t *testing.T, f *turnReceiptFixture, retry memory.Turn) {
	t.Helper()
	outcome, err := f.runtime.IngestSequencedTurn(context.Background(), memory.SequencedTurn{
		ProducerID: "old-producer", ScopeSequence: 1,
		Fingerprint: f.record(t).Fingerprint, Turn: retry,
	})

	code, _, ok := memory.ClassifyError(err)
	if !ok || code != memory.CodeConflict || outcome.Kind != "" || f.calls != 1 || f.processCalls != 1 {
		t.Fatalf("matching fingerprint bypassed missing lineage proof: outcome=%+v error=%v", outcome, err)
	}
}
