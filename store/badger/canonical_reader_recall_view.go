package badger

import (
	"context"
	"errors"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func (r *CanonicalReader) loadCanonicalView(
	ctx context.Context,
	scope memory.Scope,
	revisionID string,
	activeOnly bool,
) (canonicalRecallView, bool, error) {
	if err := r.validateCanonicalViewRequest(scope, revisionID); err != nil {
		return canonicalRecallView{}, false, err
	}

	if r.store.restoreBlocked.Load() {
		return canonicalRecallView{}, false, restoreUnavailable()
	}

	r.store.maintenanceMu.RLock()
	defer r.store.maintenanceMu.RUnlock()

	if r.store.restoreBlocked.Load() {
		return canonicalRecallView{}, false, restoreUnavailable()
	}

	view := canonicalRecallView{state: memory.RevisionStateActive}

	err := r.store.db.View(func(txn *badger.Txn) error {
		if err := loadCanonicalViewCore(txn, scope, revisionID, activeOnly, &view); err != nil {
			return err
		}

		if err := checkDeniedRevision(txn, scope, view.revision.RevisionID); err != nil {
			return err
		}

		return loadCanonicalViewSources(txn, scope, &view)
	})
	if errors.Is(err, badger.ErrKeyNotFound) || errors.Is(err, errCanonicalInactive) ||
		errors.Is(err, errCanonicalDenied) {
		return canonicalRecallView{}, false, nil
	}

	if err != nil {
		return canonicalRecallView{}, false, canonicalStoreError("load canonical recall view", err)
	}

	return view, true, nil
}

func (r *CanonicalReader) validateCanonicalViewRequest(scope memory.Scope, revisionID string) error {
	if err := scope.Validate(); err != nil {
		return err
	}

	if strings.TrimSpace(revisionID) == "" {
		return memory.PermanentError(memory.CodeInvalidDerived, "canonical revision id is required", nil)
	}

	if !r.hasOpenStore() {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical reader store is closed", nil)
	}

	return nil
}

func (r *CanonicalReader) hasOpenStore() bool { return r != nil && r.store != nil && r.store.db != nil }
