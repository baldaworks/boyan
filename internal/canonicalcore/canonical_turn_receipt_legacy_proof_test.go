package canonicalcore_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"

	m "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func TestTurnReceiptLegacyProofStrength(t *testing.T) {
	for _, full := range []bool{false, true} {
		name := "lossy-native-header"
		if full {
			name = "full-raw-turn"
		}

		t.Run(name, func(t *testing.T) { testLegacyReceiptProofStrength(t, full) })
	}
}

func testLegacyReceiptProofStrength(t *testing.T, full bool) {
	t.Helper()
	f := newTurnReceiptFixture(t)

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
	sourceKey := turnReceiptRecordKey(t, f, "source", f.turn.SourceID)
	privacyKey := turnReceiptRecordKey(t, f, "source-privacy", f.turn.SourceID)
	payloadKey, err := m.TupleKey(0x53, 1, "payload", "internal:payload", string(m.ScopeKindPersonal), source.Payload.ID)
	requireNoError(t, err, "payload key: %v")
	err = f.store.DB().Update(func(txn *badger.Txn) error {
		privacy, err := receiptFixturePrivacy(txn, privacyKey)
		if err != nil {
			return err
		}

		privacy["payload"], err = json.Marshal(source.Payload)
		if err != nil {
			return err
		}

		for _, record := range []struct {
			key   []byte
			kind  string
			value any
		}{
			{sourceKey, "source", source}, {privacyKey, "source-privacy", privacy}, {payloadKey, "payload", data},
		} {
			if err := putReceiptFixtureRecord(txn, record.key, record.kind, record.value); err != nil {
				return err
			}
		}

		return nil
	})
	requireNoError(t, err, "full legacy source: %v")
}

func putReceiptFixtureRecord(txn *badger.Txn, key []byte, kind string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}

	encoded, err := m.MarshalRecordEnvelope(kind, payload)
	if err != nil {
		return err
	}

	return txn.Set(key, encoded)
}

func receiptFixturePrivacy(txn *badger.Txn, key []byte) (map[string]json.RawMessage, error) {
	item, err := txn.Get(key)
	if err != nil {
		return nil, err
	}

	encoded, err := item.ValueCopy(nil)
	if err != nil {
		return nil, err
	}

	envelope, err := m.UnmarshalRecordEnvelope(encoded)
	if err != nil {
		return nil, err
	}

	var privacy map[string]json.RawMessage
	if err := json.Unmarshal(envelope.Payload, &privacy); err != nil {
		return nil, err
	}

	return privacy, nil
}
