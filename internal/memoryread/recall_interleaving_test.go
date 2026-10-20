//go:build integration

package memoryread_test

import (
	"context"
	"os"
	"testing"
	"time"

	index "github.com/baldaworks/boyan/index/sqlite"
	core "github.com/baldaworks/boyan/internal/canonicalcore"
	read "github.com/baldaworks/boyan/internal/memoryread"
	projection "github.com/baldaworks/boyan/internal/projection"
	m "github.com/baldaworks/boyan/pkg/memory"
)

type interleavingExtractor struct{}

func (interleavingExtractor) ExtractCanonicalSemantics(
	_ context.Context, r m.CanonicalExtractionRequest,
) ([]m.SemanticCandidate, error) {
	msg := r.Turn.Messages[0]

	evidence, err := m.NewEvidenceRef(
		r.Turn.SourceID, msg.MessageID, msg.Role, msg.Text, 0, uint32(len(msg.Text)), m.AssertionModeUser,
	)
	if err != nil {
		return nil, err
	}

	return []m.SemanticCandidate{{
		Kind: m.MemoryKindState, Subject: msg.Text, Predicate: "memory",
		Memory: m.MemoryCandidate{
			Kind: m.MemoryKindState, Statement: msg.Text, Temporal: m.Temporal{ObservedAt: r.Turn.CompletedAt},
			Evidence: []m.EvidenceRef{evidence}, Sensitivity: m.SensitivityStandard, Retention: m.RetentionClassStandard,
		},
	}}, nil
}

type interleavingProjection struct {
	*index.Store

	before func()
}

func (p *interleavingProjection) SearchRecallConsistent(
	ctx context.Context, r m.RecallRequest,
) ([]m.RecallProjectionHit, error) {
	if p.before != nil {
		f := p.before
		p.before = nil

		f()
	}

	return p.Store.SearchRecallConsistent(ctx, r)
}

func newInterleavingRecall(t *testing.T, newCanonical recallOwnerFactory) (*read.RecallService, m.RecallRequest) {
	t.Helper()

	store, reader := func() (recallOwner, m.RecallCanonicalReader) {
		return newCanonical(t)
	}()

	t.Cleanup(func() { _ = store.Close() })

	processor, err := core.NewCanonicalTurnProcessor(
		store, interleavingExtractor{}, m.PolicyRegistry{Version: "policy-v1"},
	)
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}

	idx, err := index.Open(t.Context(), root, index.Options{})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = idx.Close() })

	applier, err := index.NewSQLiteCanonicalApplier(idx, reader, store)
	if err != nil {
		t.Fatal(err)
	}

	syncer, err := projection.NewProjectionRuntime(store, store, applier, "sqlite", 128)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = syncer.Close(context.Background()) })

	scope := m.Scope{Key: "test:interleaving", Kind: m.ScopeKindPersonal}
	at := time.Now().UTC().Add(-time.Hour)
	fixture := recallCommitFixture{processor: processor, syncer: syncer, scope: scope}
	fixture.commit(t, "first", "memory first", at)

	advancing := &interleavingProjection{
		Store:  idx,
		before: func() { fixture.commit(t, "second", "memory second", at.Add(time.Minute)) },
	}

	service, err := read.NewRecallService(reader, advancing)
	if err != nil {
		t.Fatal(err)
	}

	request := m.RecallRequest{Scope: scope, Query: "memory", Limit: 10, MinScopeChangeSeq: 1}

	return service, request
}

type recallCommitFixture struct {
	processor *core.CanonicalTurnProcessor
	syncer    *projection.ProjectionRuntime
	scope     m.Scope
}

func (f recallCommitFixture) commit(t *testing.T, id, text string, at time.Time) {
	t.Helper()

	conversation := m.ConversationRef{ConversationID: "conversation", AgentConversationID: "agent"}

	turn, err := m.NewTurn(f.scope, conversation, id, at, text, "OK")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.processor.ProcessTurn(context.Background(), turn, m.LegacyDerivationRef()); err != nil {
		t.Fatal(err)
	}

	if err := f.syncer.Sync(context.Background(), f.scope); err != nil {
		t.Fatal(err)
	}
}

func TestRecallCommitInterleavingReturnsRetryableConflict(t *testing.T) {
	runRecallAuthorities(t, func(t *testing.T, newCanonical recallOwnerFactory) {
		service, request := newInterleavingRecall(t, newCanonical)
		ctx := context.Background()
		response, err := service.Search(ctx, request)
		code, class, ok := m.ClassifyError(err)

		if response.ScopeChangeSeq != 0 || len(response.Results) != 0 {
			t.Fatalf("partial response returned: %+v", response)
		}

		if !ok || code != m.CodeConflict || class != m.ErrorClassRetryable {
			t.Fatalf("expected retryable interleaving conflict: %v", err)
		}

		response, err = service.Search(ctx, request)
		if err != nil || len(response.Results) != 2 || response.ScopeChangeSeq != 2 {
			t.Fatalf("retry control: %+v %v", response, err)
		}
	})
}
