package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	. "github.com/baldaworks/boyan/pkg/memory"
)

func (p *CanonicalTurnProcessor) buildCanonicalMutation(turn Turn, derivation DerivationRef, scopeState ScopeState, active []ActiveCanonicalMemory, activeView []ActiveMemoryItem, candidates []SemanticCandidate) (CanonicalMutation, error) {
	sourceID := turn.SourceID
	if !isCanonicalID(sourceID) {
		return CanonicalMutation{}, invalidDerived("canonical turn source identity is invalid")
	}

	sourcePayload, err := json.Marshal(canonicalTurnSourcePayload{
		ExportID:       turn.ExportID,
		SessionID:      turn.Session.SessionID,
		AgentSessionID: turn.Session.AgentSessionID,
		SourceTurnID:   turn.SourceTurnID,
		CompletedAt:    turn.CompletedAt,
		TerminalStatus: normalizedTerminalStatus(turn),
	})
	if err != nil {
		return CanonicalMutation{}, PermanentError(CodeInvalidDerived, "encode canonical source payload", err)
	}

	operationID, err := CanonicalSemanticOperationID(turn.ExportID, derivation)
	if err != nil {
		return CanonicalMutation{}, err
	}
	mutation := CanonicalMutation{
		SchemaVersion:        CanonicalSchemaVersionV1,
		Scope:                turn.Scope,
		ExpectedScopeVersion: scopeState.Version,
		Operation: OperationRecord{
			OperationID: operationID,
			Fingerprint: mustCanonicalFingerprint(turn.ExportID, derivation),
			CommittedAt: turn.CompletedAt,
		},
	}

	sourcePayloadRecord, sourceRef, err := p.payload(canonicalPayloadID("source", sourceID), sourcePayload)
	if err != nil {
		return CanonicalMutation{}, err
	}
	mutation.Sources = []CanonicalSourceRecord{{SourceID: sourceID, Scope: turn.Scope, Sensitivity: SensitivityStandard, Retention: RetentionClassStandard, Payload: sourceRef}}
	mutation.Payloads = append(mutation.Payloads, sourcePayloadRecord)

	messageByID := make(map[string]Message, len(turn.Messages))
	for _, message := range turn.Messages {
		if message.MessageID == "" {
			return CanonicalMutation{}, invalidDerived("canonical turn message identity is required")
		}
		if _, exists := messageByID[message.MessageID]; exists {
			return CanonicalMutation{}, invalidDerived("canonical turn message identity is duplicated")
		}
		messageByID[message.MessageID] = message
		payload, payloadRef, payloadErr := p.payload(canonicalPayloadID("message", message.MessageID), []byte(message.Text))
		if payloadErr != nil {
			return CanonicalMutation{}, payloadErr
		}
		mutation.Messages = append(mutation.Messages, MessageRecord{MessageID: message.MessageID, SourceID: sourceID, Role: message.Role, Payload: payloadRef})
		mutation.Payloads = append(mutation.Payloads, payload)
	}

	seenItems := make(map[string]struct{}, len(candidates))
	seenRevisions := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		canonicalCandidate, err := canonicalizeCandidate(sourceID, messageByID, candidate)
		if err != nil {
			return CanonicalMutation{}, err
		}
		reconciliation, err := p.policy.ReconcileActive(turn.Scope, canonicalCandidate, activeView)
		if err != nil {
			return CanonicalMutation{}, err
		}
		if _, exists := seenItems[reconciliation.Item.ItemID]; exists {
			return CanonicalMutation{}, invalidDerived("canonical semantic candidates resolve to one item more than once")
		}
		if _, exists := seenRevisions[reconciliation.RevisionID]; exists {
			return CanonicalMutation{}, invalidDerived("canonical semantic candidates contain duplicate revisions")
		}
		seenItems[reconciliation.Item.ItemID] = struct{}{}
		seenRevisions[reconciliation.RevisionID] = struct{}{}

		parentRevision, revisionNumber, err := activeRevision(active, reconciliation.Supersedes)
		if err != nil {
			return CanonicalMutation{}, err
		}
		revisionPayload, revisionRef, err := p.payload(canonicalPayloadID("revision", reconciliation.RevisionID), []byte(canonicalCandidate.Memory.Statement))
		if err != nil {
			return CanonicalMutation{}, err
		}
		revision := MemoryRevision{
			SchemaVersion: MemorySchemaVersion,
			RevisionID:    reconciliation.RevisionID,
			ItemID:        reconciliation.Item.ItemID,
			Revision:      revisionNumber,
			Temporal:      canonicalCandidate.Memory.Temporal,
			Evidence:      append([]EvidenceRef(nil), reconciliation.Provenance...),
			Sensitivity:   canonicalCandidate.Memory.Sensitivity,
			Retention:     canonicalCandidate.Memory.Retention,
			Payload:       revisionRef,
		}
		if parentRevision != nil {
			revision.Parents = []string{parentRevision.RevisionID}
		}
		if reconciliation.Action == ReconciliationActionCreate {
			mutation.Items = append(mutation.Items, reconciliation.Item)
		}
		mutation.Revisions = append(mutation.Revisions, revision)
		mutation.Lifecycle = append(mutation.Lifecycle, reconciliation.Lifecycle)
		mutation.Heads = append(mutation.Heads, ItemHead{ItemID: reconciliation.Item.ItemID, RevisionID: reconciliation.RevisionID})
		mutation.Payloads = append(mutation.Payloads, revisionPayload)
		mutation.Operation.Outcome = append(mutation.Operation.Outcome, reconciliation.RevisionID)
	}

	if err := mutation.Validate(); err != nil {
		return CanonicalMutation{}, err
	}
	return mutation, nil
}

type canonicalTurnSourcePayload struct {
	ExportID       string             `json:"export_id"`
	SessionID      string             `json:"session_id"`
	AgentSessionID string             `json:"agent_session_id"`
	SourceTurnID   string             `json:"source_turn_id"`
	CompletedAt    time.Time          `json:"completed_at"`
	TerminalStatus TurnTerminalStatus `json:"terminal_status"`
}

func (p *CanonicalTurnProcessor) payload(payloadID string, plaintext []byte) (CanonicalPayload, PayloadRef, error) {
	if len(plaintext) == 0 {
		return CanonicalPayload{}, PayloadRef{}, invalidDerived("canonical payload is empty")
	}
	digest := sha256.Sum256(plaintext)
	ref := PayloadRef{ID: payloadID, Digest: hex.EncodeToString(digest[:]), ByteSize: uint32(len(plaintext))}
	payload := CanonicalPayload{Ref: ref, Data: append([]byte(nil), plaintext...)}
	if err := payload.Validate(); err != nil {
		return CanonicalPayload{}, PayloadRef{}, err
	}
	return payload, ref, nil
}

func canonicalPayloadID(kind, identity string) string {
	return reconciliationID("payload", kind, identity)
}

func mustCanonicalFingerprint(exportID string, derivation DerivationRef) string {
	return reconciliationID("fingerprint", exportID, derivation.Pipeline, derivation.Policy, derivation.Prompt, derivation.Model)
}

func normalizedTerminalStatus(turn Turn) TurnTerminalStatus {
	if strings.TrimSpace(string(turn.TerminalStatus)) == "" {
		return TurnTerminalStatusSuccess
	}
	return turn.TerminalStatus
}
