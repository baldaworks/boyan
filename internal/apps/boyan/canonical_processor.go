package app

import (
	"context"

	. "github.com/baldaworks/boyan/pkg/memory"
)

// CanonicalTurnProcessor owns the canonical turn-processing dependencies. Its
// implementation is split so orchestration and mutation assembly remain easy
// to read independently.
type CanonicalTurnProcessor struct {
	store     CanonicalStore
	extractor CanonicalSemanticExtractor
	policy    PolicyRegistry
}

// NewCanonicalTurnProcessor validates the mandatory cutover dependencies.
func NewCanonicalTurnProcessor(store CanonicalStore, extractor CanonicalSemanticExtractor, policy PolicyRegistry) (*CanonicalTurnProcessor, error) {
	if store == nil || extractor == nil {
		return nil, PermanentError(CodeStoreFailure, "canonical processor dependencies are required", nil)
	}
	if !isCanonicalID(policy.Version) {
		return nil, invalidDerived("canonical reconciliation policy version is required")
	}
	return &CanonicalTurnProcessor{store: store, extractor: extractor, policy: policy}, nil
}

// ProcessTurn extracts bounded semantics from one terminal turn and commits
// the source, messages, and derived revisions as one canonical mutation. The
// model supplies only semantic fields and evidence claims; all identities,
// payload references, revision numbers, and scope state are derived here.
func (p *CanonicalTurnProcessor) ProcessTurn(ctx context.Context, turn Turn, derivation DerivationRef) (CanonicalMutationOutcome, error) {
	if p == nil || p.store == nil || p.extractor == nil {
		return CanonicalMutationOutcome{}, PermanentError(CodeDisabled, "canonical turn processor is unavailable", nil)
	}
	if ctx == nil {
		return CanonicalMutationOutcome{}, invalidDerived("canonical turn processing context is required")
	}
	if err := turn.Validate(); err != nil {
		return CanonicalMutationOutcome{}, err
	}
	turn = cloneTurn(turn)
	expectedSourceID := TurnSourceID(turn.ExportID)
	if turn.SourceID != "" && turn.SourceID != expectedSourceID {
		return CanonicalMutationOutcome{}, PermanentError(CodePermanent, "canonical turn source identity does not match export", nil)
	}
	turn.SourceID = expectedSourceID
	for index := range turn.Messages {
		if turn.Messages[index].MessageID != "" {
			continue
		}
		if turn.Messages[index].Role == MessageRoleTool {
			turn.Messages[index].MessageID = TurnToolMessageID(turn.ExportID, turn.Messages[index].ToolName, turn.Messages[index].ToolCallID)
		} else {
			turn.Messages[index].MessageID = TurnMessageID(turn.ExportID, turn.Messages[index].Role)
		}
	}
	if err := derivation.Validate(); err != nil {
		return CanonicalMutationOutcome{}, err
	}
	if turnTextExceeds(turn, MaxDerivedTurnTextBytes) {
		return CanonicalMutationOutcome{}, limitExceeded("canonical turn text exceeds the derived processing limit")
	}
	if err := checkContext(ctx); err != nil {
		return CanonicalMutationOutcome{}, err
	}

	scopeState, err := p.store.LoadScopeState(ctx, turn.Scope)
	if err != nil {
		return CanonicalMutationOutcome{}, err
	}
	if scopeState.Scope != turn.Scope {
		return CanonicalMutationOutcome{}, PermanentError(CodeScopeViolation, "canonical scope state does not match turn", nil)
	}

	active, err := p.store.ScanActiveMemory(ctx, ActiveMemoryScanRequest{
		Scope: turn.Scope,
		Limit: maxCanonicalReadRecords,
	})
	if err != nil {
		return CanonicalMutationOutcome{}, err
	}
	activeView := make([]ActiveMemoryItem, len(active))
	for i, memory := range active {
		if err := validateActiveCanonicalMemory(turn.Scope, memory); err != nil {
			return CanonicalMutationOutcome{}, err
		}
		activeView[i] = ActiveMemoryItem{Item: memory.Item, Evidence: append([]EvidenceRef(nil), memory.Evidence...)}
	}

	candidates, err := p.extractor.ExtractCanonicalSemantics(ctx, CanonicalExtractionRequest{
		SchemaVersion: CanonicalSchemaVersionV1,
		Derivation:    derivation,
		Turn:          cloneTurn(turn),
	})
	if err != nil {
		return CanonicalMutationOutcome{}, err
	}
	if len(candidates) > MaxCandidateCount {
		return CanonicalMutationOutcome{}, limitExceeded("canonical semantic candidate count exceeds the limit")
	}

	mutation, err := p.buildCanonicalMutation(turn, derivation, scopeState, active, activeView, candidates)
	if err != nil {
		return CanonicalMutationOutcome{}, err
	}
	outcome, err := p.store.ApplyCanonicalMutation(ctx, mutation)
	if err != nil {
		return CanonicalMutationOutcome{}, err
	}
	if err := outcome.Validate(); err != nil {
		return CanonicalMutationOutcome{}, err
	}
	return outcome, nil
}
