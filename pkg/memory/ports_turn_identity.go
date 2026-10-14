package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
)

// CanonicalTurnCertificate binds immutable input and ordered message membership.
// Only this content-free certificate is persisted; errors never include its hash.
type CanonicalTurnCertificate struct {
	Version     uint32   `json:"version"`
	SourceID    string   `json:"source_id"`
	Fingerprint string   `json:"fingerprint"`
	MessageIDs  []string `json:"message_ids"`
}

// CanonicalTurnIdentityCommit carries transient input for transaction-level proof.
// TurnIdentity and Turn are excluded from mutation serialization.
type CanonicalTurnIdentityCommit struct {
	Certificate CanonicalTurnCertificate
	Turn        Turn `json:"-"`
}

// CanonicalTurnIdentityResult returns reusable immutable structural records only.
type CanonicalTurnIdentityResult struct {
	// FullInputVerified distinguishes complete immutable input proof from lossy legacy reconstruction.
	FullInputVerified bool
	Found             bool
	Forgotten         bool
	Source            CanonicalSourceRecord
	Messages          []MessageRecord
}

// CanonicalTurnIdentityStore is optional; CanonicalStore remains portable.
type CanonicalTurnIdentityStore interface {
	VerifyCanonicalTurn(ctx context.Context, turn Turn) (CanonicalTurnIdentityResult, error)
}

// Validate checks bounded, versioned, content-free certification metadata.
func (c CanonicalTurnCertificate) Validate() error {
	if c.Version != CanonicalTurnFingerprintVersion || !isCanonicalID(c.SourceID) || len(c.Fingerprint) != sha256.Size*2 {
		return invalidDerived("canonical turn certificate identity is invalid")
	}

	if _, err := hex.DecodeString(c.Fingerprint); err != nil {
		return invalidDerived("canonical turn certificate fingerprint is invalid")
	}

	if len(c.MessageIDs) < 1 || len(c.MessageIDs) > maxTurnToolMessages+2 {
		return invalidDerived("canonical turn certificate message bound is invalid")
	}

	return validateUniqueCanonicalIDs(c.MessageIDs, "canonical turn certificate messages")
}

// Validate binds the transient authoritative input to its certificate and scope.
func (c CanonicalTurnIdentityCommit) Validate(scope Scope) error {
	if err := c.Certificate.Validate(); err != nil {
		return err
	}

	turn, err := NormalizeCanonicalTurn(c.Turn)
	if err != nil {
		return err
	}

	if turn.Scope != scope || turn.SourceID != c.Certificate.SourceID {
		return PermanentError(CodeScopeViolation, "canonical turn certificate scope or source differs", nil)
	}

	fingerprint, err := CanonicalTurnFingerprint(turn)
	if err != nil {
		return err
	}

	ids := make([]string, 0, len(turn.Messages))
	for _, message := range turn.Messages {
		ids = append(ids, message.MessageID)
	}

	if fingerprint != c.Certificate.Fingerprint || !slices.Equal(ids, c.Certificate.MessageIDs) {
		return PermanentError(CodeConflict, "canonical turn certificate does not match authoritative input", nil)
	}

	return nil
}
