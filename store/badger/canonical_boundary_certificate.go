package badger

import (
	"context"
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

const badgerRecordBoundaryCertificate = "boundary-certificate"

// VerifyCanonicalBoundary checks content-free immutable boundary input proof.
// Uncertified input requires bounded proof that its scope supports certification.
func (s *CanonicalStore) VerifyCanonicalBoundary(ctx context.Context,
	boundary memory.Boundary) (memory.BoundaryIdentityResult, error) {
	if err := canonicalContextError(ctx); err != nil {
		return memory.BoundaryIdentityResult{}, err
	}

	identity, err := memory.NewCanonicalBoundaryIdentityCommit(boundary)
	if err != nil {
		return memory.BoundaryIdentityResult{}, err
	}

	if s == nil {
		return memory.BoundaryIdentityResult{}, boundaryCertificateStoreFailure()
	}

	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()

	if s.db == nil {
		return memory.BoundaryIdentityResult{}, boundaryCertificateStoreFailure()
	}

	var result memory.BoundaryIdentityResult

	err = s.db.View(func(txn *badger.Txn) error {
		var err error

		result, err = verifyCanonicalBoundaryTxn(txn, identity)

		return err
	})
	if err != nil {
		return memory.BoundaryIdentityResult{}, canonicalStoreError("verify canonical boundary identity", err)
	}

	return result, canonicalContextError(ctx)
}

func verifyCanonicalBoundaryTxn(txn *badger.Txn,
	identity memory.CanonicalBoundaryIdentityCommit) (memory.BoundaryIdentityResult, error) {
	certificate, found, err := loadBoundaryCertificate(txn, identity.Boundary.Scope, identity.Certificate.ExportID)
	if err != nil {
		return memory.BoundaryIdentityResult{}, err
	}

	if found && certificate != identity.Certificate {
		return memory.BoundaryIdentityResult{}, boundaryIdentityConflict()
	}

	if found {
		return memory.BoundaryIdentityResult{Certified: true}, nil
	}

	return classifyBoundaryScope(txn, identity.Boundary.Scope)
}

func loadBoundaryCertificate(txn *badger.Txn, scope memory.Scope,
	exportID string) (memory.CanonicalBoundaryCertificate, bool, error) {
	key, err := canonicalKey(scope, badgerRecordBoundaryCertificate, exportID)
	if err != nil {
		return memory.CanonicalBoundaryCertificate{}, false, err
	}

	var certificate memory.CanonicalBoundaryCertificate

	err = getCanonicalRecord(txn, key, badgerRecordBoundaryCertificate, &certificate)
	if errors.Is(err, badger.ErrKeyNotFound) {
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

func prepareBoundaryIdentity(txn *badger.Txn, scope memory.Scope,
	identity *memory.CanonicalBoundaryIdentityCommit, operationID string, operationKey []byte) error {
	if identity == nil {
		return rejectUnprovenDerivedOperation(txn, scope, operationID)
	}

	if err := identity.Validate(scope); err != nil {
		return err
	}

	result, err := verifyCanonicalBoundaryTxn(txn, *identity)
	if err != nil || result.Certified {
		return err
	}
	// A committed operation without original-input proof cannot be certified by
	// a later retry, even when its operation fingerprint still matches.
	if _, err := txn.Get(operationKey); err == nil {
		return boundaryIdentityConflict()
	} else if !errors.Is(err, badger.ErrKeyNotFound) {
		return err
	}

	return nil
}

func persistBoundaryCertificate(txn *badger.Txn, scope memory.Scope,
	identity *memory.CanonicalBoundaryIdentityCommit) error {
	if identity == nil {
		return nil
	}

	key, err := canonicalKey(scope, badgerRecordBoundaryCertificate, identity.Certificate.ExportID)
	if err != nil {
		return err
	}

	certificate, found, err := loadBoundaryCertificate(txn, scope, identity.Certificate.ExportID)
	if err != nil {
		return err
	}

	if found {
		if certificate != identity.Certificate {
			return boundaryIdentityConflict()
		}

		return nil
	}

	if err := persistBoundaryCapability(txn, scope); err != nil {
		return err
	}

	return putCanonicalImmutableRecord(txn, key, badgerRecordBoundaryCertificate, identity.Certificate)
}

// A conflicting write may have raced a different input's first certificate.
// Inspect that winner in a fresh read transaction while the caller already
// holds maintenanceMu, avoiding nested public store locks.
func boundaryCertificateWriteError(db *badger.DB,
	identity *memory.CanonicalBoundaryIdentityCommit, writeErr error) error {
	if identity == nil || !errors.Is(writeErr, badger.ErrConflict) {
		return writeErr
	}

	err := db.View(func(txn *badger.Txn) error {
		_, err := verifyCanonicalBoundaryTxn(txn, *identity)

		return err
	})
	if err != nil {
		return err
	}

	return writeErr
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
