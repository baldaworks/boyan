package canonicalcore_test

import (
	"context"
	"encoding/json"
	"testing"

	core "github.com/baldaworks/boyan/internal/canonicalcore"
	m "github.com/baldaworks/boyan/pkg/memory"
	b "github.com/baldaworks/boyan/store/badger"
	"github.com/dgraph-io/badger/v4"
)

// These exercise real producers as well as the direct mutation regressions in
// store/badger. None can bypass the common atomic classification writer.
func TestSourcePrivacyNativeDerivationAndReconciliationWriters(t *testing.T) {
	for _, sameSource := range []bool{true, false} {
		t.Run(map[bool]string{true: "later-derivation", false: "reconciliation"}[sameSource], func(t *testing.T) {
			_, _, turn, _, store := nativeFlow(t)
			assertWriterPrivacy(t, store, turn.Scope, turn.SourceID, "standard")

			extractor := canonicalExtractorFunc(func(ctx context.Context, request m.CanonicalExtractionRequest) (
				[]m.SemanticCandidate, error,
			) {
				candidates, err := extractTestStateCandidate(ctx, request)
				if err == nil {
					candidates[0].Memory.Sensitivity = m.SensitivitySensitive
					if sameSource {
						candidates[0].Predicate = "sensitive-other-fact"
					}
				}

				return candidates, err
			})
			processor, err := core.NewCanonicalTurnProcessor(store, extractor, m.PolicyRegistry{Version: "policy-v1"})
			requireNoError(t, err, "processor: %v")

			update := turn
			if !sameSource {
				update, err = m.NewTurn(turn.Scope, turn.Conversation, "new-sensitive-turn",
					turn.CompletedAt.Add(1), "Я живу в Оше", "OK")
				requireNoError(t, err, "turn: %v")
			}

			derivation := testProcessorDerivation()
			derivation.Model = "sensitive-model"
			outcome, err := processor.ProcessTurn(context.Background(), update, derivation)
			requireNoError(t, err, "sensitive writer: %v")
			assertWriterPrivacy(t, store, turn.Scope, turn.SourceID, "sensitive")
			assertWriterPrivacy(t, store, update.Scope, update.SourceID, "sensitive")
			replay, err := processor.ProcessTurn(context.Background(), update, derivation)
			requireNoError(t, err, "replay: %v")

			if replay.ScopeVersion != outcome.ScopeVersion {
				t.Fatal("replay changed canonical version")
			}

			assertWriterPrivacy(t, store, turn.Scope, turn.SourceID, "sensitive")
		})
	}
}

func TestSourcePrivacyBoundaryCompatibilityWritersAndReplay(t *testing.T) {
	f := newBoundaryReplayFixture(t, false)
	turn := modelRetryTurn(t)
	assertWriterPrivacy(t, f.store, turn.Scope, turn.SourceID, "standard")
	_, err := f.processor.ProcessBoundary(context.Background(), f.export)
	requireNoError(t, err, "boundary: %v")
	assertWriterPrivacy(t, f.store, turn.Scope, turn.SourceID, "standard")
	f.restart(t)
	_, err = f.processor.ProcessBoundary(context.Background(), f.export)
	requireNoError(t, err, "boundary replay: %v")
	assertWriterPrivacy(t, f.store, turn.Scope, turn.SourceID, "standard")
}

func assertWriterPrivacy(t *testing.T, store *b.CanonicalStore, scope m.Scope, sourceID, want string) {
	t.Helper()

	key, err := m.TupleKey(0x53, 1, "source-privacy", scope.Key, string(scope.Kind), sourceID)
	requireNoError(t, err, "key: %v")

	var authority struct {
		State    string  `json:"state"`
		Scope    m.Scope `json:"scope"`
		SourceID string  `json:"source_id"`
	}

	err = store.DB().View(func(txn *badger.Txn) error {
		item, readErr := txn.Get(key)
		if readErr != nil {
			return readErr
		}

		encoded, readErr := item.ValueCopy(nil)
		if readErr != nil {
			return readErr
		}

		envelope, readErr := m.UnmarshalRecordEnvelope(encoded)
		if readErr != nil {
			return readErr
		}

		return json.Unmarshal(envelope.Payload, &authority)
	})
	requireNoError(t, err, "authority: %v")

	if authority.State != want || authority.Scope != scope || authority.SourceID != sourceID {
		t.Fatalf("authority=%+v, want %s", authority, want)
	}
}
