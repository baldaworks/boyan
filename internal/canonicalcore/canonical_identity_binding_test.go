package canonicalcore_test

import (
	"context"
	"testing"
	"time"

	core "github.com/baldaworks/boyan/internal/canonicalcore"
	m "github.com/baldaworks/boyan/pkg/memory"
)

type identityBindingStore struct {
	*processorTestStore

	proof m.CanonicalTurnIdentityResult
}

func (s *identityBindingStore) VerifyCanonicalTurn(context.Context, m.Turn) (m.CanonicalTurnIdentityResult, error) {
	return s.proof, nil
}

func TestCanonicalProcessorPreservesCertifiedLegacyPayloadReferences(t *testing.T) {
	t.Parallel()

	scope := m.Scope{Key: "test:legacy-binding", Kind: m.ScopeKindPersonal}
	turn, err := m.NewTurn(scope, m.ConversationRef{ConversationID: "conversation", AgentConversationID: "agent"},
		"turn", time.Now().UTC(), "user", "assistant")
	requireNoError(t, err, "turn: %v")

	source := m.CanonicalSourceRecord{SourceID: turn.SourceID, Scope: scope,
		Sensitivity: m.SensitivityStandard, Retention: m.RetentionClassStandard,
		Payload: m.PayloadRef{ID: "old-source-payload", Digest: "old-source-digest", ByteSize: 1}}

	messages := make([]m.MessageRecord, len(turn.Messages))
	for index, message := range turn.Messages {
		messages[index] = m.MessageRecord{MessageID: message.MessageID, SourceID: turn.SourceID, Role: message.Role,
			Payload: m.PayloadRef{ID: "old-" + string(message.Role), Digest: "old-digest", ByteSize: 1}}
	}

	store := &identityBindingStore{processorTestStore: &processorTestStore{
		state: m.ScopeState{SchemaVersion: m.CanonicalSchemaVersionV1, Scope: scope, Version: 1, ChangeSeq: 1}},
		proof: m.CanonicalTurnIdentityResult{Found: true, Source: source, Messages: messages}}
	processor, err := core.NewCanonicalTurnProcessor(store, canonicalExtractorFunc(extractTestStateCandidate),
		m.PolicyRegistry{Version: "policy-v1"})
	requireNoError(t, err, "processor: %v")
	_, err = processor.ProcessTurn(context.Background(), turn, testProcessorDerivation())
	requireNoError(t, err, "derive existing source: %v")

	mutation := store.mutation
	if mutation.Sources[0].Payload != source.Payload || mutation.Messages[0].Payload != messages[0].Payload {
		t.Fatal("verified immutable source/message references were rewritten")
	}

	if mutation.TurnIdentity == nil || len(mutation.TurnIdentity.Certificate.MessageIDs) != len(turn.Messages) {
		t.Fatal("mutation lacks its ordered authoritative input certificate")
	}

	if len(mutation.Payloads) != 1 || mutation.Payloads[0].Ref != mutation.Revisions[0].Payload {
		t.Fatal("rederivation attempts to rewrite verified legacy source/message bytes")
	}
}
