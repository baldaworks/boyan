package standaloneruntime //nolint:testpackage // Verifies storage recovery precedes production projection startup.

import (
	"context"
	"testing"
	"time"

	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestStandaloneStartupResumesPendingEraseBeforeProjection(t *testing.T) {
	runtime, err := New(testStandaloneConfig(t))
	requireNoError(t, err, "runtime: %v")

	defer runtime.Stop(context.Background())

	scope := m.Scope{Key: "test:pending-erase", Kind: m.ScopeKindPersonal}
	_, err = runtime.service.store.BeginScopeErase(context.Background(), m.ScopeEraseRequest{
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
