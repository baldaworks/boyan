//go:build integration

package canonicalcore_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestTurnReceiptLegacyProofStrength(t *testing.T) {
	runCoreAuthorities(t, func(t *testing.T, factory coreFactory) {
		for _, full := range []bool{false, true} {
			name := "lossy-native-header"
			if full {
				name = "full-raw-turn"
			}

			t.Run(name, func(t *testing.T) { testLegacyReceiptProofStrength(t, full, factory) })
		}
	})
}

func testLegacyReceiptProofStrength(t *testing.T, full bool, factory coreFactory) {
	t.Helper()
	f := newTurnReceiptFixture(t, factory)

	raw := legacyReceiptWireTurn(f.turn)
	if response := f.post(t, raw); response.Code != http.StatusOK {
		t.Fatalf("initial: %s", response.Body)
	}

	original := f.record(t)
	if full {
		replaceReceiptSourceWithFullTurn(t, f)
	}

	deleteTurnReceiptRecord(t, f, "turn-certificate", f.turn.SourceID)
	before, err := f.store.LoadScopeState(context.Background(), f.turn.Scope)
	requireNoError(t, err, "state: %v")

	for _, retry := range []m.Turn{raw, f.turn} {
		response := f.post(t, retry)

		want := http.StatusConflict
		if full {
			want = http.StatusOK
		}

		if response.Code != want {
			t.Fatalf("legacy proof=%t status=%d body=%s", full, response.Code, response.Body)
		}

		if full {
			assertTurnReceiptReplayUnchanged(t, f, original, before, response)
		}
	}

	if f.calls != 1 || f.processCalls != 1 {
		t.Fatal("legacy receipt entered processor")
	}
}

func replaceReceiptSourceWithFullTurn(t *testing.T, f *turnReceiptFixture) {
	t.Helper()

	proof, err := f.store.VerifyCanonicalTurn(context.Background(), f.turn)
	requireNoError(t, err, "original proof: %v")

	source := proof.Source
	data, err := json.Marshal(f.turn)
	requireNoError(t, err, "raw turn: %v")

	digest := sha256.Sum256(data)
	source.Payload.Digest = hex.EncodeToString(digest[:])
	source.Payload.ByteSize = uint32(len(data))

	var privacy map[string]json.RawMessage

	privacyKey := coreSQLKey(canonicalsql.SourcePrivacy, f.turn.Scope, []byte(f.turn.SourceID))
	readCoreRecord(t, f.store, privacyKey, &privacy)
	privacy["payload"], err = json.Marshal(source.Payload)
	requireNoError(t, err, "privacy payload: %v")
	writeCoreSQLRecord(t, f.store, coreSQLKey(canonicalsql.Source, f.turn.Scope, []byte(f.turn.SourceID)), source)
	writeCoreSQLRecord(t, f.store, privacyKey, privacy)
	writeCoreSQLRecord(t, f.store, coreSQLKey(canonicalsql.Payload, m.Scope{}, []byte(source.Payload.ID)), data)

	return
}
