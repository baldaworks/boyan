package badger

import (
	"context"
	"errors"
	"reflect"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func traceDenied() error {
	return memory.PermanentError(memory.CodeForgotten, "canonical trace is unavailable", nil)
}

func authorizeTraceSource(txn *badger.Txn, scope memory.Scope, id string) (memory.CanonicalSourceRecord, error) {
	if err := checkDeniedSource(txn, scope, id); err != nil {
		return memory.CanonicalSourceRecord{}, err
	}

	source, found, err := loadHistoricalPrivacySource(txn, scope, id)
	if err != nil {
		return source, err
	}

	if !found {
		return source, traceDenied()
	}

	if err := requireTraceScopePrivacy(txn, scope); err != nil {
		return source, err
	}

	record, found, err := loadHistoricalSourcePrivacy(txn, source)
	if err != nil {
		return source, err
	}

	if !found || record.Preparing || record.State != sourcePrivacyStandard ||
		source.Sensitivity != memory.SensitivityStandard {
		return source, traceDenied()
	}

	return source, nil
}

func (r *CanonicalReader) traceScopeVersion(txn *badger.Txn, scope memory.Scope) (uint64, error) {
	if err := r.store.requireSourcePrivacyReady(txn); err != nil {
		return 0, err
	}

	if err := checkScopeEraseFence(txn, scope, ""); err != nil {
		return 0, err
	}

	if err := checkScopeErasePayloadGuard(txn); err != nil {
		return 0, err
	}

	key, err := badgerScopeKey(scope)
	if err != nil {
		return 0, err
	}

	state, err := loadCanonicalOperationScopeState(txn, scope, key)

	return state.Version, err
}

func (r *CanonicalReader) traceReadTxn(ctx context.Context, read func(*badger.Txn) error) error {
	if err := canonicalReaderContext(ctx); err != nil {
		return err
	}

	if r.store.restoreBlocked.Load() || !r.store.maintenanceMu.TryRLock() {
		return restoreUnavailable()
	}
	defer r.store.maintenanceMu.RUnlock()

	if r.store.db == nil {
		return invalidSourcePrivacy()
	}

	return r.store.db.View(func(txn *badger.Txn) error {
		if err := canonicalReaderContext(ctx); err != nil {
			return err
		}

		return read(txn)
	})
}

// No payload reads follow this transaction. Its snapshot is the successful
// trace linearization point; subsequent writes cannot invalidate that read.
func (r *CanonicalReader) finalTracePrivacyBarrier(ctx context.Context, scope memory.Scope, graph traceGraph) error {
	return r.traceReadTxn(ctx, func(txn *badger.Txn) error {
		version, err := r.traceScopeVersion(txn, scope)
		if err != nil {
			return err
		}

		if err := recheckTraceSources(txn, scope, graph); err != nil {
			return err
		}

		if err := recheckTraceRevisions(txn, scope, graph); err != nil {
			return err
		}

		if version != graph.scopeVersion {
			return traceStateChanged()
		}

		return canonicalReaderContext(ctx)
	})
}

// Trace errors never retain payload, evidence or storage decoder diagnostics.
func publicTraceError(err error) error {
	if errors.Is(err, errCanonicalDenied) || errors.Is(err, badger.ErrKeyNotFound) {
		return traceDenied()
	}

	code, retryable, ok := memory.ClassifyError(err)
	if !ok {
		return invalidSourcePrivacy()
	}

	if retryable == memory.ErrorClassRetryable {
		return memory.RetryableError(code, "canonical trace is unavailable", nil)
	}

	return memory.PermanentError(code, "canonical trace is unavailable", nil)
}

func traceStateChanged() error {
	return memory.RetryableError(memory.CodeConflict, "canonical trace state changed", nil)
}

func recheckTraceSources(txn *badger.Txn, scope memory.Scope, graph traceGraph) error {
	for id, captured := range graph.sourceIDs {
		current, err := authorizeTraceSource(txn, scope, id)
		if err != nil {
			return err
		}

		if current.Payload != captured.Payload {
			return traceStateChanged()
		}
	}

	return nil
}

func recheckTraceRevisions(txn *badger.Txn, scope memory.Scope, graph traceGraph) error {
	for _, node := range graph.nodes {
		var view canonicalRecallView
		if err := loadTraceViewCore(txn, scope, node.view.revision.RevisionID, &view); err != nil {
			return err
		}

		if !reflect.DeepEqual(view.revision, node.view.revision) {
			return traceStateChanged()
		}
	}

	return nil
}

func requireTraceScopePrivacy(txn *badger.Txn, scope memory.Scope) error {
	incomplete, err := privacyScopeIncomplete(txn, scope)
	if err != nil {
		return err
	}

	if incomplete {
		return traceDenied()
	}

	return nil
}
