package canonicalsql

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func verifyCanonicalBoundaryTxn(ctx context.Context, txn *Tx,
	identity memory.CanonicalBoundaryIdentityCommit) (memory.BoundaryIdentityResult, error) {
	certificate, found, err := loadBoundaryCertificate(ctx, txn, identity.Boundary.Scope, identity.Certificate.ExportID)
	if err != nil {
		return memory.BoundaryIdentityResult{}, err
	}

	if found && certificate != identity.Certificate {
		return memory.BoundaryIdentityResult{}, boundaryIdentityConflict()
	}

	if found {
		return memory.BoundaryIdentityResult{Certified: true}, nil
	}

	return classifyBoundaryScope(ctx, txn, identity.Boundary.Scope)
}

func loadBoundaryCertificate(ctx context.Context, txn *Tx, scope memory.Scope,
	exportID string) (memory.CanonicalBoundaryCertificate, bool, error) {
	key, err := privacyKey(scope, BoundaryCertificate, exportID)
	if err != nil {
		return memory.CanonicalBoundaryCertificate{}, false, err
	}

	var certificate memory.CanonicalBoundaryCertificate

	err = getPrivacyRecord(ctx, txn, key, BoundaryCertificate, &certificate)
	if privacyMissing(err) {
		return certificate, false, nil
	}

	if err != nil {
		return certificate, false, boundaryCertificateStoreFailure()
	}

	if err := certificate.Validate(); err != nil || certificate.ExportID != exportID {
		return certificate, false, boundaryCertificateStoreFailure()
	}

	return certificate, true, nil
}

func prepareBoundaryIdentity(ctx context.Context, txn *Tx, scope memory.Scope,
	identity *memory.CanonicalBoundaryIdentityCommit, operationID string, operationKey Key) error {
	if identity == nil {
		return rejectUnprovenDerivedOperation(ctx, txn, scope, operationID)
	}

	if err := identity.Validate(scope); err != nil {
		return err
	}

	result, err := verifyCanonicalBoundaryTxn(ctx, txn, *identity)
	if err != nil || result.Certified {
		return err
	}
	// A committed operation without original-input proof cannot be certified by
	// a later retry, even when its operation fingerprint still matches.
	if _, err := txn.Get(ctx, operationKey); err == nil {
		return boundaryIdentityConflict()
	} else if !privacyMissing(err) {
		return err
	}

	return nil
}

func persistBoundaryCertificate(ctx context.Context, txn *Tx, scope memory.Scope,
	identity *memory.CanonicalBoundaryIdentityCommit) error {
	if identity == nil {
		return nil
	}

	key, err := privacyKey(scope, BoundaryCertificate, identity.Certificate.ExportID)
	if err != nil {
		return err
	}

	certificate, found, err := loadBoundaryCertificate(ctx, txn, scope, identity.Certificate.ExportID)
	if err != nil {
		return err
	}

	if found {
		if certificate != identity.Certificate {
			return boundaryIdentityConflict()
		}

		return nil
	}

	if err := persistBoundaryCapability(ctx, txn, scope); err != nil {
		return err
	}

	return putImmutablePrivacyRecord(ctx, txn, key, identity.Certificate)
}

func cloneBoundaryIdentity(identity *memory.CanonicalBoundaryIdentityCommit) *memory.CanonicalBoundaryIdentityCommit {
	if identity == nil {
		return nil
	}

	cloned := *identity

	return &cloned
}

func boundaryIdentityConflict() error {
	return memory.PermanentError(memory.CodeConflict, "canonical boundary authoritative input differs or is unproven", nil)
}

func boundaryCertificateStoreFailure() error {
	return memory.PermanentError(memory.CodeStoreFailure, "canonical boundary certificate is unavailable or invalid", nil)
}
