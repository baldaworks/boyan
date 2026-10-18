package canonicalsql

import (
	"context"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

const (
	boundaryCapabilityID            = "v1"
	legacyDerivedOperationPrefix    = "boyan-derived:v1:operation:"
	legacyDerivedOperationHashBytes = 64
)

type boundaryCapability struct {
	Version uint32       `json:"version"`
	Scope   memory.Scope `json:"scope"`
}

func classifyBoundaryScope(ctx context.Context, txn *Tx, scope memory.Scope) (memory.BoundaryIdentityResult, error) {
	found, err := loadBoundaryCapability(ctx, txn, scope)
	if err != nil {
		return memory.BoundaryIdentityResult{}, err
	}

	if found {
		return memory.BoundaryIdentityResult{SafelyNew: true}, nil
	}

	found, err = txn.HasLegacyOperationNamespace(ctx, scope)
	if err != nil {
		return memory.BoundaryIdentityResult{}, err
	}

	if found {
		return memory.BoundaryIdentityResult{}, boundaryIdentityConflict()
	}

	return memory.BoundaryIdentityResult{SafelyNew: true}, nil
}

func loadBoundaryCapability(ctx context.Context, txn *Tx, scope memory.Scope) (bool, error) {
	key, err := privacyKey(scope, BoundaryCapability, boundaryCapabilityID)
	if err != nil {
		return false, err
	}

	var marker boundaryCapability

	err = getPrivacyRecord(ctx, txn, key, BoundaryCapability, &marker)
	if privacyMissing(err) {
		return false, nil
	}

	if err != nil || marker.Version != 1 || marker.Scope != scope {
		return false, boundaryCertificateStoreFailure()
	}

	return true, nil
}

func persistBoundaryCapability(ctx context.Context, txn *Tx, scope memory.Scope) error {
	key, err := privacyKey(scope, BoundaryCapability, boundaryCapabilityID)
	if err != nil {
		return err
	}

	return putImmutablePrivacyRecord(
		ctx, txn, key, boundaryCapability{Version: 1, Scope: scope},
	)
}

// Generic operations must not add unproven derived history after a scope has
// promised certificate-aware writes. Native operation namespaces stay usable.
func rejectUnprovenDerivedOperation(ctx context.Context, txn *Tx, scope memory.Scope, operationID string) error {
	if len(operationID) != len(legacyDerivedOperationPrefix)+legacyDerivedOperationHashBytes ||
		!strings.HasPrefix(operationID, legacyDerivedOperationPrefix) {
		return nil
	}

	found, err := loadBoundaryCapability(ctx, txn, scope)
	if err != nil {
		return err
	}

	if found {
		return boundaryIdentityConflict()
	}

	return nil
}
