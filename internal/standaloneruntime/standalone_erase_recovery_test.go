package standaloneruntime //nolint:testpackage // Verifies storage recovery precedes production projection startup.

import (
	"context"
	"testing"
	"time"

	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestStandaloneStartupResumesPendingEraseBeforeProjection(t *testing.T) {
	forEachSQLRuntimeConfig(t, testStandaloneStartupResumesPendingEraseBeforeProjection)
}

func assertStandaloneRecoveredErase(t *testing.T, runtime *Runtime, original m.ScopeEraseJournal) {
	t.Helper()

	journal, found, err := runtime.service.store.LoadScopeErase(context.Background(), original.Scope, original.OperationID)
	requireNoError(t, err, "recovered: %v")

	if !runtime.Ready() || !found || journal.Stage != m.ScopeEraseComplete {
		t.Fatalf("startup not recovered: %+v", journal)
	}

	if journal.SourceCount == 0 || journal.RevisionCount == 0 {
		t.Fatalf("startup not recovered: %+v", journal)
	}
}

func testStandaloneStartupResumesPendingEraseBeforeProjection(t *testing.T, cfg Config) {
	runtime, err := New(cfg)
	requireNoError(t, err, "runtime: %v")

	defer runtime.Stop(context.Background())

	scope := m.Scope{Key: "test:pending-erase", Kind: m.ScopeKindPersonal}

	requireNoError(t, runtime.service.store.PrepareSourcePrivacy(t.Context()), "prepare fixture: %v")
	_, err = runtimeStore(t, runtime).BeginScopeErase(context.Background(), m.ScopeEraseRequest{
		Scope: scope, OperationID: "pending-erase", Fingerprint: "pending-erase-fingerprint", ForgottenAt: time.Now().UTC(),
	})
	requireNoError(t, err, "begin erase: %v")
	requireNoError(t, runtime.service.startComponents(context.Background()), "start components: %v")
	journal, found, err := runtime.service.store.LoadScopeErase(context.Background(), scope, "pending-erase")
	requireNoError(t, err, "load resumed journal: %v")

	if !found || journal.Stage != m.ScopeEraseComplete {
		t.Fatalf("startup left unfinished journal: %+v", journal)
	}
}
