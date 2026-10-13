package badger

import (
	"errors"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

const (
	badgerRecordBoundaryCapability  = "boundary-capability"
	boundaryCapabilityID            = "v1"
	legacyDerivedOperationPrefix    = "boyan-derived:v1:operation:"
	legacyDerivedOperationHashBytes = 64
)

type boundaryCapability struct {
	Version uint32       `json:"version"`
	Scope   memory.Scope `json:"scope"`
}

func classifyBoundaryScope(txn *badger.Txn, scope memory.Scope) (memory.BoundaryIdentityResult, error) {
	found, err := loadBoundaryCapability(txn, scope)
	if err != nil {
		return memory.BoundaryIdentityResult{}, err
	}

	if found {
		return memory.BoundaryIdentityResult{SafelyNew: true}, nil
	}

	prefix, err := legacyDerivedNamespacePrefix(scope)
	if err != nil {
		return memory.BoundaryIdentityResult{}, err
	}

	options := badger.DefaultIteratorOptions
	options.PrefetchValues = false
	options.Prefix = prefix

	iterator := txn.NewIterator(options)
	defer iterator.Close()
	// The encoded component length is fixed before truncating its hash bytes.
	// Any first matching key is unproven, even if its hash or value is corrupt.
	// Never advance: proving namespace existence requires at most one key.
	iterator.Seek(prefix)

	if iterator.ValidForPrefix(prefix) {
		return memory.BoundaryIdentityResult{}, boundaryIdentityConflict()
	}

	return memory.BoundaryIdentityResult{SafelyNew: true}, nil
}

func legacyDerivedNamespacePrefix(scope memory.Scope) ([]byte, error) {
	key, err := canonicalKey(scope, badgerRecordOperation,
		legacyDerivedOperationPrefix+strings.Repeat("0", legacyDerivedOperationHashBytes))
	if err != nil {
		return nil, err
	}

	return key[:len(key)-legacyDerivedOperationHashBytes], nil
}

func loadBoundaryCapability(txn *badger.Txn, scope memory.Scope) (bool, error) {
	key, err := canonicalKey(scope, badgerRecordBoundaryCapability, boundaryCapabilityID)
	if err != nil {
		return false, err
	}

	var marker boundaryCapability

	err = getCanonicalRecord(txn, key, badgerRecordBoundaryCapability, &marker)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return false, nil
	}

	if err != nil || marker.Version != 1 || marker.Scope != scope {
		return false, boundaryCertificateStoreFailure()
	}

	return true, nil
}

func persistBoundaryCapability(txn *badger.Txn, scope memory.Scope) error {
	key, err := canonicalKey(scope, badgerRecordBoundaryCapability, boundaryCapabilityID)
	if err != nil {
		return err
	}

	return putCanonicalImmutableRecord(
		txn, key, badgerRecordBoundaryCapability, boundaryCapability{Version: 1, Scope: scope},
	)
}

// Generic operations must not add unproven derived history after a scope has
// promised certificate-aware writes. Native operation namespaces stay usable.
func rejectUnprovenDerivedOperation(txn *badger.Txn, scope memory.Scope, operationID string) error {
	if len(operationID) != len(legacyDerivedOperationPrefix)+legacyDerivedOperationHashBytes ||
		!strings.HasPrefix(operationID, legacyDerivedOperationPrefix) {
		return nil
	}

	found, err := loadBoundaryCapability(txn, scope)
	if err != nil {
		return err
	}

	if found {
		return boundaryIdentityConflict()
	}

	return nil
}
