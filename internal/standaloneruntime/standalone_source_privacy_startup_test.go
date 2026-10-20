//go:build integration

package standaloneruntime //nolint:testpackage // Tests private startup readiness ordering against an old store.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	m "github.com/baldaworks/boyan/pkg/memory"
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

	_, connection := openRuntimeCanonicalSQL(t, runtime.service.cfg)

	err = connection.View(t.Context(), func(tx *canonicalsql.Tx) error {
		_, err := tx.Get(t.Context(), canonicalsql.Key{Family: canonicalsql.SourcePrivacyReady})

		return err
	})
	if code, _, _ := m.ClassifyError(err); code != m.CodeNotFound {
		t.Fatalf("canceled startup published privacy readiness: %v", err)
	}
}

func markRuntimePrivacyUnprepared(t *testing.T, runtime *Runtime) {
	t.Helper()

	scope := m.Scope{Key: "test:old-startup", Kind: m.ScopeKindPersonal}
	key := canonicalsql.Key{Family: canonicalsql.ScopeState, Scope: scope}
	payload, err := canonicalsql.Encode(canonicalsql.ScopeState,
		m.ScopeState{SchemaVersion: m.CanonicalSchemaVersionV1, Scope: scope})
	requireNoError(t, err, "envelope: %v")

	_, connection := openRuntimeCanonicalSQL(t, runtime.service.cfg)
	err = connection.Update(t.Context(), func(tx *canonicalsql.Tx) error {
		if err := tx.Put(t.Context(), key, payload); err != nil {
			return err
		}

		return tx.Delete(t.Context(), canonicalsql.Key{Family: canonicalsql.SourcePrivacyReady})
	})
	requireNoError(t, err, "old fixture: %v")
}
