package boyan_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	hostapi "github.com/baldaworks/boyan/pkg/boyan"
	m "github.com/baldaworks/boyan/pkg/memory"
	b "github.com/baldaworks/boyan/store/badger"
	db "github.com/dgraph-io/badger/v4"
)

func TestPublicHostFinishesEraseBeforeRebuildingLostIndex(t *testing.T) {
	config := testHostConfig(t)
	target := capabilityTestTurn(t, m.Scope{Key: "startup:erase", Kind: m.ScopeKindPersonal})
	foreign := target
	foreign.Scope.Kind = m.ScopeKindGroup
	targetOutcome := persistIntegrationTurn(t, config, target)
	foreignOutcome := persistIntegrationTurn(t, config, foreign)
	request, refs, root := interruptStartupErase(t, config, target, targetOutcome)
	err := os.Remove(filepath.Join(config.StateDir, "search", "search.sqlite"))
	requireNoError(t, err, "remove lost disposable index: %v")
	host, err := hostapi.New(context.Background(), config)
	requireNoError(t, err, "host: %v")
	t.Cleanup(func() { _ = host.Close() })
	requireNoError(t, host.Start(context.Background()), "startup must finish erase before repair: %v")
	assertStartupScopeRecall(t, host, target, targetOutcome.ChangeSeq+1, 0)
	assertStartupScopeRecall(t, host, foreign, foreignOutcome.ChangeSeq, 1)

	_, err = host.Trace(context.Background(), m.TraceRequest{Scope: target.Scope,
		Root: root, MaxNodes: 10})
	if err == nil {
		t.Fatal("startup rebuilt forgotten trace")
	}

	requireNoError(t, host.Close(), "close: %v")
	assertStartupEraseComplete(t, config, request, refs, foreign)
}

func interruptStartupErase(t *testing.T, config hostapi.Config, turn m.Turn,
	outcome m.CanonicalMutationOutcome,
) (m.ScopeEraseRequest, []m.PayloadRef, m.RevisionRef) {
	t.Helper()

	store, err := b.Open(filepath.Join(config.StateDir, "badger"))

	requireNoError(t, err, "store: %v")
	defer func() { requireNoError(t, store.Close(), "store close: %v") }()

	proof, err := store.VerifyCanonicalTurn(context.Background(), turn)
	requireNoError(t, err, "identity: %v")

	refs := []m.PayloadRef{proof.Source.Payload}
	for _, message := range proof.Messages {
		refs = append(refs, message.Payload)
	}

	revisions, err := store.LoadCanonicalRevisions(context.Background(), m.CanonicalRevisionReadRequest{
		Scope: turn.Scope, RevisionIDs: outcome.RevisionIDs,
	})
	requireNoError(t, err, "revisions: %v")

	for _, revision := range revisions {
		refs = append(refs, revision.Payload)
	}

	request := m.ScopeEraseRequest{Scope: turn.Scope, OperationID: "startup-interrupted-erase",
		Fingerprint: "startup-interrupted-fingerprint", ForgottenAt: time.Now().UTC(),
		ExpectedScopeVersion: outcome.ScopeVersion}
	journal, err := store.BeginScopeErase(context.Background(), request)
	requireNoError(t, err, "begin: %v")

	for journal.Stage != m.ScopeEraseScrubMessages {
		journal, err = store.AdvanceScopeErase(context.Background(), turn.Scope, request.OperationID)
		requireNoError(t, err, "advance: %v")

		if journal.Stage == m.ScopeEraseComplete {
			t.Fatal("fixture missed interrupted scrub boundary")
		}
	}

	return request, refs, m.RevisionRef{ItemID: revisions[0].ItemID, RevisionID: revisions[0].RevisionID}
}

func assertStartupScopeRecall(t *testing.T, host *hostapi.Host, turn m.Turn, minimum uint64, count int) {
	t.Helper()

	response, err := host.Search(context.Background(), m.RecallRequest{
		Scope: turn.Scope, Query: "Ada", Limit: 10, MinScopeChangeSeq: minimum,
	})
	requireNoError(t, err, "honest startup recall: %v")

	if len(response.Results) != count {
		t.Fatalf("scope %+v recall: %+v", turn.Scope, response)
	}
}

func assertStartupEraseComplete(t *testing.T, config hostapi.Config, request m.ScopeEraseRequest,
	refs []m.PayloadRef, foreign m.Turn,
) {
	t.Helper()

	store, err := b.Open(filepath.Join(config.StateDir, "badger"))

	requireNoError(t, err, "store: %v")
	defer func() { requireNoError(t, store.Close(), "store close: %v") }()

	journal, found, err := store.LoadScopeErase(context.Background(), request.Scope, request.OperationID)
	requireNoError(t, err, "journal: %v")

	if !found || journal.Stage != m.ScopeEraseComplete {
		t.Fatalf("unfinished startup erase: %+v", journal)
	}

	for _, ref := range refs {
		_, err := store.LoadPayload(context.Background(), ref)
		if !errors.Is(err, db.ErrKeyNotFound) {
			t.Fatalf("startup retained target payload %s", ref.ID)
		}
	}

	proof, err := store.VerifyCanonicalTurn(context.Background(), foreign)
	requireNoError(t, err, "foreign identity: %v")

	if !proof.Found || proof.Forgotten {
		t.Fatal("startup erase changed foreign scope kind")
	}
}
