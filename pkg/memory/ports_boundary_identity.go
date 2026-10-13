package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// CanonicalBoundaryCertificate is content-free immutable replay evidence.
// Exact scope is supplied by its storage key and bound inside the fingerprint.
type CanonicalBoundaryCertificate struct {
	Version     uint32 `json:"version"`
	ExportID    string `json:"export_id"`
	Fingerprint string `json:"fingerprint"`
}

// CanonicalBoundaryIdentityCommit carries transient input for atomic validation.
// Authoritative Boundary content is never serialized into the certificate.
type CanonicalBoundaryIdentityCommit struct {
	Certificate CanonicalBoundaryCertificate
	Boundary    Boundary `json:"-"`
}

// BoundaryIdentityResult distinguishes a certified match from absent proof.
type BoundaryIdentityResult struct {
	Certified bool
	// SafelyNew permits uncertified input after bounded scope classification.
	SafelyNew bool
}

// CanonicalBoundaryIdentityStore verifies authoritative input before replay.
type CanonicalBoundaryIdentityStore interface {
	VerifyCanonicalBoundary(ctx context.Context, boundary Boundary) (BoundaryIdentityResult, error)
}

// NewCanonicalBoundaryIdentityCommit constructs proof from normalized input.
func NewCanonicalBoundaryIdentityCommit(boundary Boundary) (CanonicalBoundaryIdentityCommit, error) {
	normalized, err := NormalizeCanonicalBoundary(boundary)
	if err != nil {
		return CanonicalBoundaryIdentityCommit{}, err
	}

	fingerprint, err := CanonicalBoundaryFingerprint(normalized)
	if err != nil {
		return CanonicalBoundaryIdentityCommit{}, err
	}

	return CanonicalBoundaryIdentityCommit{Boundary: normalized,
		Certificate: CanonicalBoundaryCertificate{Version: CanonicalBoundaryFingerprintVersion,
			ExportID: normalized.ExportID, Fingerprint: fingerprint}}, nil
}

// Validate checks fixed-size, versioned certificate metadata.
func (c CanonicalBoundaryCertificate) Validate() error {
	const prefix = "boyan:v1:boundary:"
	if c.Version != CanonicalBoundaryFingerprintVersion || !strings.HasPrefix(c.ExportID, prefix) ||
		len(c.ExportID) != len(prefix)+sha256.Size*2 || len(c.Fingerprint) != sha256.Size*2 {
		return invalidDerived("canonical boundary certificate identity is invalid")
	}

	if _, err := hex.DecodeString(strings.TrimPrefix(c.ExportID, prefix)); err != nil {
		return invalidDerived("canonical boundary certificate export identity is invalid")
	}

	if _, err := hex.DecodeString(c.Fingerprint); err != nil {
		return invalidDerived("canonical boundary certificate fingerprint is invalid")
	}

	return nil
}

// Validate rederives the full authoritative proof and exact scope binding.
func (c CanonicalBoundaryIdentityCommit) Validate(scope Scope) error {
	if err := c.Certificate.Validate(); err != nil {
		return err
	}

	normalized, err := NormalizeCanonicalBoundary(c.Boundary)
	if err != nil {
		return err
	}

	if normalized.Scope != scope {
		return PermanentError(CodeScopeViolation, "canonical boundary certificate scope differs", nil)
	}

	fingerprint, err := CanonicalBoundaryFingerprint(normalized)
	if err != nil {
		return err
	}

	if normalized.ExportID != c.Certificate.ExportID || fingerprint != c.Certificate.Fingerprint {
		return PermanentError(CodeConflict, "canonical boundary certificate does not match authoritative input", nil)
	}

	return nil
}
