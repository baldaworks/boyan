package memory

import (
	"context"
	"time"
)

// ScopeEraseStage is durable progress, never an authorization supplied by callers.
type ScopeEraseStage string

const (
	ScopeEraseReadOutcomes   ScopeEraseStage = "read-outcomes"
	ScopeEraseLockSources    ScopeEraseStage = "lock-sources"
	ScopeEraseLockMessages   ScopeEraseStage = "lock-messages"
	ScopeEraseLockRevisions  ScopeEraseStage = "lock-revisions"
	ScopeEraseCheckSources   ScopeEraseStage = "check-sources"
	ScopeEraseCheckMessages  ScopeEraseStage = "check-messages"
	ScopeEraseCheckRevisions ScopeEraseStage = "check-revisions"
	ScopeEraseReleaseLocks   ScopeEraseStage = "release-locks"
	ScopeEraseRejectLocks    ScopeEraseStage = "reject-locks"
	ScopeEraseRejected       ScopeEraseStage = "rejected"
	ScopeErasePrepared       ScopeEraseStage = "prepared"
	ScopeEraseDenySources    ScopeEraseStage = "deny-sources"
	ScopeEraseDenyRevisions  ScopeEraseStage = "deny-revisions"
	ScopeEraseCommitOutcome  ScopeEraseStage = "commit-outcome"
	ScopeEraseScrubSources   ScopeEraseStage = "scrub-sources"
	ScopeEraseScrubMessages  ScopeEraseStage = "scrub-messages"
	ScopeEraseScrubRevisions ScopeEraseStage = "scrub-revisions"
	ScopeEraseScrubbed       ScopeEraseStage = "scrubbed"
	ScopeEraseComplete       ScopeEraseStage = "complete"
	// MaxScopeEraseReferences bounds journal metadata independently of model input.
	MaxScopeEraseReferences = MaxForgetOutcomeReferences
)

// ScopeEraseRequest establishes the immutable membership boundary of one erase.
type ScopeEraseRequest struct {
	Kind                 ForgetKind
	SourceID             string
	SourceRef            SourceRef
	Scope                Scope
	OperationID          string
	Fingerprint          string
	ExpectedScopeVersion uint64
	ForgottenAt          time.Time
}

// ScopeEraseJournal contains identifiers and progress only, never memory payloads.
type ScopeEraseJournal struct {
	Kind                   ForgetKind      `json:"kind,omitempty"`
	SourceID               string          `json:"source_id,omitempty"`
	SourceRef              SourceRef       `json:"source_ref,omitempty"`
	RejectionCode          ErrorCode       `json:"rejection_code,omitempty"`
	PreflightRevisionCount uint64          `json:"preflight_revision_count,omitempty"`
	LegacyOutcome          bool            `json:"legacy_outcome,omitempty"`
	LegacySourceIDs        []string        `json:"legacy_source_ids,omitempty"`
	Version                uint32          `json:"version"`
	Scope                  Scope           `json:"scope"`
	OperationID            string          `json:"operation_id"`
	Fingerprint            string          `json:"fingerprint"`
	ForgottenAt            time.Time       `json:"forgotten_at"`
	BaselineVersion        uint64          `json:"baseline_version"`
	BaselineChangeSeq      uint64          `json:"baseline_change_seq"`
	Stage                  ScopeEraseStage `json:"stage"`
	ReferenceOffset        uint32          `json:"reference_offset,omitempty"`
	Cursor                 []byte          `json:"cursor,omitempty"`
	SourceCount            uint64          `json:"source_count"`
	RevisionCount          uint64          `json:"revision_count"`
	Sources                []SourceRef     `json:"sources,omitempty"`
	Revisions              []RevisionRef   `json:"revisions,omitempty"`
	ReferencesTruncated    bool            `json:"references_truncated,omitempty"`
	Outcome                *ForgetOutcome  `json:"outcome,omitempty"`
}

// ScopeEraseStore is an optional recovery capability, not part of CanonicalStore.
// Only the backend runner can advance progress; completion verifies scrubbed state.
type ScopeEraseStore interface {
	BeginScopeErase(ctx context.Context, request ScopeEraseRequest) (ScopeEraseJournal, error)
	LoadScopeErase(ctx context.Context, scope Scope, operationID string) (ScopeEraseJournal, bool, error)
	CompleteScopeErase(ctx context.Context, scope Scope, operationID string) error
	RunScopeErase(ctx context.Context, request ScopeEraseRequest) (ScopeEraseJournal, error)
	AdvanceScopeErase(ctx context.Context, scope Scope, operationID string) (ScopeEraseJournal, error)
	ListPendingScopeErases(ctx context.Context, cursor []byte, limit int) ([]ScopeEraseJournal, []byte, error)
}

func (r ScopeEraseRequest) Validate() error {
	if err := r.Scope.Validate(); err != nil {
		return err
	}

	if !isCanonicalID(r.OperationID) || !isCanonicalID(r.Fingerprint) || r.ForgottenAt.IsZero() ||
		r.ExpectedScopeVersion == ^uint64(0) {
		return invalidDerived("scope erase request is invalid")
	}

	return r.validateTarget()
}

// Validate rejects corrupt or unbounded durable progress.
func (j ScopeEraseJournal) Validate() error {
	request := ScopeEraseRequest{
		Scope: j.Scope, OperationID: j.OperationID, Fingerprint: j.Fingerprint,
		Kind: j.Kind, SourceID: j.SourceID, SourceRef: j.SourceRef,
		ExpectedScopeVersion: j.BaselineVersion, ForgottenAt: j.ForgottenAt,
	}
	if err := request.Validate(); err != nil {
		return err
	}

	if j.Version != 1 || len(j.Cursor) > 8192 || len(j.LegacySourceIDs) > MaxScopeEraseReferences ||
		j.PreflightRevisionCount > MaxScopeEraseReferences {
		return invalidDerived("scope erase journal bounds or version are invalid")
	}

	if j.RejectionCode != "" && j.RejectionCode != CodeLimitExceeded {
		return invalidDerived("scope erase rejection code is invalid")
	}

	if err := j.validateStage(); err != nil {
		return err
	}

	if err := j.validateReferences(); err != nil {
		return err
	}

	return j.validateOutcome()
}

// ForgetKind preserves scope behavior for journals written without a kind.
func (j ScopeEraseJournal) ForgetKind() ForgetKind {
	if j.Kind == "" {
		return ForgetKindScope
	}

	return j.Kind
}

func (j ScopeEraseJournal) validateStage() error {
	switch j.Stage {
	case ScopeEraseReadOutcomes, ScopeEraseLockSources, ScopeEraseLockMessages, ScopeEraseLockRevisions,
		ScopeEraseCheckSources, ScopeEraseCheckMessages, ScopeEraseCheckRevisions,
		ScopeEraseReleaseLocks, ScopeEraseRejectLocks, ScopeEraseRejected,
		ScopeErasePrepared, ScopeEraseDenySources, ScopeEraseDenyRevisions, ScopeEraseCommitOutcome,
		ScopeEraseScrubSources, ScopeEraseScrubMessages, ScopeEraseScrubRevisions, ScopeEraseScrubbed, ScopeEraseComplete:
		return nil
	default:
		return invalidDerived("scope erase journal stage is invalid")
	}
}

func (j ScopeEraseJournal) validateReferences() error {
	if len(j.Sources)+len(j.Revisions) > MaxScopeEraseReferences {
		return invalidDerived("scope erase references exceed bound")
	}

	if j.ReferencesTruncated && (len(j.Sources) != 0 || len(j.Revisions) != 0) {
		return invalidDerived("truncated scope erase has references")
	}

	if j.SourceCount < uint64(len(j.Sources)) || j.RevisionCount < uint64(len(j.Revisions)) {
		return invalidDerived("scope erase counts are invalid")
	}

	return j.validateReferenceScopes()
}

func (j ScopeEraseJournal) validateReferenceScopes() error {
	for _, ref := range j.Sources {
		if err := ref.Validate(); err != nil {
			return err
		}

		if ref.Scope != j.Scope {
			return invalidDerived("scope erase source scope differs")
		}
	}

	for _, ref := range j.Revisions {
		if err := ref.Validate(); err != nil {
			return err
		}
	}

	return nil
}

func (j ScopeEraseJournal) validateOutcome() error {
	switch j.Stage {
	case ScopeEraseScrubSources,
		ScopeEraseScrubMessages,
		ScopeEraseScrubRevisions,
		ScopeEraseScrubbed,
		ScopeEraseReleaseLocks,
		ScopeEraseComplete:
		if j.Outcome == nil {
			return invalidDerived("scope erase outcome is missing")
		}
	}

	if j.Outcome != nil {
		if err := j.Outcome.Validate(); err != nil {
			return err
		}

		if j.Outcome.Scope != j.Scope || j.Outcome.OperationID != j.OperationID || j.Outcome.Kind != j.ForgetKind() {
			return invalidDerived("scope erase outcome identity differs")
		}
	}

	return nil
}

func (r ScopeEraseRequest) validateTarget() error {
	if r.Kind == "" || r.Kind == ForgetKindScope {
		if r.SourceID != "" || r.SourceRef != (SourceRef{}) {
			return invalidDerived("scope erase has a source selector")
		}

		return nil
	}

	if r.Kind != ForgetKindSource || !isCanonicalID(r.SourceID) {
		return invalidDerived("erase target kind or source identity is invalid")
	}

	if err := r.SourceRef.Validate(); err != nil {
		return err
	}

	if r.SourceRef.Scope != r.Scope {
		return invalidDerived("erase source selector scope differs")
	}

	return nil
}
