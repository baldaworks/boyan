package standaloneruntime //nolint:testpackage // Tests private startup readiness ordering against an old store.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	m "github.com/baldaworks/boyan/pkg/memory"
	canonicalbadger "github.com/baldaworks/boyan/store/badger"
	"github.com/dgraph-io/badger/v4"
)

func TestRuntimePreparesOldSourcePrivacyBeforeServing(t *testing.T) {
	runtime, err := New(testStandaloneConfig(t))
	requireNoError(t, err, "new: %v")
	t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
	markRuntimePrivacyUnprepared(t, runtime)

	request := m.CanonicalOperationCommitRequest{Scope: m.Scope{Key: "test:old-startup", Kind: m.ScopeKindPersonal},
		OperationID: "before-start", Fingerprint: "before-start", CommittedAt: time.Now().UTC()}

	_, err = runtime.service.store.CommitCanonicalOperation(context.Background(), request)
	if code, _, ok := m.ClassifyError(err); !ok || code != m.CodeUnavailable {
		t.Fatalf("old writer started before preparation: %v", err)
	}

	requireNoError(t, runtime.Start(context.Background()), "start: %v")

	response := httptest.NewRecorder()
	runtime.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://boyan/readyz", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("prepared runtime not ready: %d", response.Code)
	}

	_, err = runtime.service.store.CommitCanonicalOperation(context.Background(), request)
	requireNoError(t, err, "prepared writer: %v")
}

func TestCanceledRuntimeStartupDoesNotPrepareOldSourcePrivacy(t *testing.T) {
	runtime, err := New(testStandaloneConfig(t))
	requireNoError(t, err, "new: %v")
	t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
	markRuntimePrivacyUnprepared(t, runtime)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := runtime.Start(ctx); err == nil {
		t.Fatal("canceled old startup succeeded")
	}

	if runtime.Ready() {
		t.Fatal("canceled startup became ready")
	}

	key, _ := m.TupleKey(0x53, 1, "source-privacy-ready")
	store, openErr := canonicalbadger.OpenCanonicalStore(runtime.service.state.CanonicalDir)
	requireNoError(t, openErr, "reopen: %v")
	t.Cleanup(func() { _ = store.Close() })

	err = store.DB().View(func(txn *badger.Txn) error {
		_, err := txn.Get(key)

		return err
	})
	if !errors.Is(err, badger.ErrKeyNotFound) {
		t.Fatalf("canceled startup published privacy readiness: %v", err)
	}
}

func markRuntimePrivacyUnprepared(t *testing.T, runtime *Runtime) {
	t.Helper()

	scope := m.Scope{Key: "test:old-startup", Kind: m.ScopeKindPersonal}
	key, _ := m.TupleKey(0x53, 1, "revision", scope.Key, string(scope.Kind), "opaque-old")
	payload, err := m.MarshalRecordEnvelope("revision", []byte(`{"old":"opaque"}`))
	requireNoError(t, err, "envelope: %v")

	marker, _ := m.TupleKey(0x53, 1, "source-privacy-ready")
	err = runtime.service.store.DB().Update(func(txn *badger.Txn) error {
		if err := txn.Set(key, payload); err != nil {
			return err
		}

		return txn.Delete(marker)
	})
	requireNoError(t, err, "old fixture: %v")
}
