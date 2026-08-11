package canonicalcore

import (
	"context"
	"testing"
	"time"

	. "github.com/baldaworks/boyan/pkg/memory"
)

func TestCanonicalTurnProcessorPersistsGroundedFailedTurn(t *testing.T) {
	t.Parallel()

	scope := Scope{Key: "telegram:1:0", Kind: ScopeKindPersonal}
	conversation := ConversationRef{ConversationID: "conversation-1", AgentConversationID: "agent-1"}
	turn, err := NewTerminalTurn(scope, conversation, "turn-1", time.Date(2026, time.August, 6, 1, 2, 3, 0, time.UTC), "Я живу в Бишкеке", "", TurnTerminalStatusFailed)
	if err != nil {
		t.Fatalf("NewTerminalTurn() error = %v", err)
	}

	extractor := canonicalExtractorFunc(func(_ context.Context, request CanonicalExtractionRequest) ([]SemanticCandidate, error) {
		message := request.Turn.Messages[0]
		evidence, evidenceErr := NewEvidenceRef(request.Turn.SourceID, message.MessageID, message.Role, message.Text, 0, uint32(len(message.Text)), AssertionModeUser)
		if evidenceErr != nil {
			return nil, evidenceErr
		}
		return []SemanticCandidate{{
			Kind:      MemoryKindState,
			Subject:   "owner",
			Predicate: "lives in",
			Memory: MemoryCandidate{
				Kind:        MemoryKindState,
				Statement:   "Я живу в Бишкеке",
				Temporal:    Temporal{ObservedAt: request.Turn.CompletedAt},
				Evidence:    []EvidenceRef{evidence},
				Sensitivity: SensitivityStandard,
				Retention:   RetentionClassStandard,
			},
		}}, nil
	})
	store := &processorTestStore{state: ScopeState{SchemaVersion: CanonicalSchemaVersionV1, Scope: scope}}
	processor, err := NewCanonicalTurnProcessor(store, extractor, PolicyRegistry{Version: "policy-v1"})
	if err != nil {
		t.Fatalf("NewCanonicalTurnProcessor() error = %v", err)
	}

	outcome, err := processor.ProcessTurn(context.Background(), turn, DerivationRef{Pipeline: "pipeline-v1", Policy: "policy-v1", Prompt: "prompt-v1", Model: "model-v1"})
	if err != nil {
		t.Fatalf("ProcessTurn() error = %v", err)
	}
	if outcome.ScopeVersion != 1 || len(outcome.RevisionIDs) != 1 {
		t.Fatalf("ProcessTurn() outcome = %+v", outcome)
	}
	if len(store.mutation.Sources) != 1 || len(store.mutation.Messages) != 1 || len(store.mutation.Revisions) != 1 {
		t.Fatalf("canonical mutation records = %+v", store.mutation)
	}
	if store.mutation.Revisions[0].Evidence[0].Role != MessageRoleUser || store.mutation.Revisions[0].Evidence[0].TextDigest == "" {
		t.Fatalf("canonical evidence = %+v", store.mutation.Revisions[0].Evidence)
	}
	if store.mutation.Operation.Outcome[0] != store.mutation.Revisions[0].RevisionID {
		t.Fatalf("operation outcome = %+v", store.mutation.Operation.Outcome)
	}
}

func TestCanonicalTurnProcessorAdvancesActiveRevision(t *testing.T) {
	t.Parallel()

	scope := Scope{Key: "telegram:1:0", Kind: ScopeKindPersonal}
	conversation := ConversationRef{ConversationID: "conversation-1", AgentConversationID: "agent-1"}
	completedAt := time.Date(2026, time.August, 6, 1, 2, 3, 0, time.UTC)
	store := &processorTestStore{state: ScopeState{SchemaVersion: CanonicalSchemaVersionV1, Scope: scope}}
	extractor := canonicalExtractorFunc(func(_ context.Context, request CanonicalExtractionRequest) ([]SemanticCandidate, error) {
		message := request.Turn.Messages[0]
		evidence, err := NewEvidenceRef(request.Turn.SourceID, message.MessageID, message.Role, message.Text, 0, uint32(len(message.Text)), AssertionModeUser)
		if err != nil {
			return nil, err
		}
		return []SemanticCandidate{{
			Kind: MemoryKindState, Subject: "owner", Predicate: "lives in",
			Memory: MemoryCandidate{Kind: MemoryKindState, Statement: message.Text, Temporal: Temporal{ObservedAt: request.Turn.CompletedAt}, Evidence: []EvidenceRef{evidence}, Sensitivity: SensitivityStandard, Retention: RetentionClassStandard},
		}}, nil
	})
	processor, err := NewCanonicalTurnProcessor(store, extractor, PolicyRegistry{Version: "policy-v1"})
	if err != nil {
		t.Fatalf("NewCanonicalTurnProcessor() error = %v", err)
	}
	firstTurn, err := NewTerminalTurn(scope, conversation, "turn-1", completedAt, "Я живу в Бишкеке", "Готово", TurnTerminalStatusSuccess)
	if err != nil {
		t.Fatalf("NewTerminalTurn(first) error = %v", err)
	}
	if _, err := processor.ProcessTurn(context.Background(), firstTurn, testProcessorDerivation()); err != nil {
		t.Fatalf("ProcessTurn(first) error = %v", err)
	}
	secondTurn, err := NewTerminalTurn(scope, conversation, "turn-2", completedAt.Add(time.Minute), "Я живу в Оше", "Готово", TurnTerminalStatusSuccess)
	if err != nil {
		t.Fatalf("NewTerminalTurn(second) error = %v", err)
	}
	if _, err := processor.ProcessTurn(context.Background(), secondTurn, testProcessorDerivation()); err != nil {
		t.Fatalf("ProcessTurn(second) error = %v", err)
	}
	if len(store.mutation.Revisions) != 1 || store.mutation.Revisions[0].Revision != 2 || len(store.mutation.Revisions[0].Parents) != 1 {
		t.Fatalf("superseding revision = %+v", store.mutation.Revisions)
	}
	if len(store.mutation.Items) != 0 {
		t.Fatalf("superseding mutation rewrote item metadata: %+v", store.mutation.Items)
	}
}
