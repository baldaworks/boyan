package bleve_test

import (
	"context"
	b "github.com/baldaworks/boyan/index/bleve"
	m "github.com/baldaworks/boyan/pkg/memory"
	"testing"
	"time"
)

func TestNativeKindFilter(t *testing.T) {
	p, err := b.Open(t.TempDir())
	requireNoError(t, err, "%v")

	defer p.Close()

	scope := m.Scope{Key: "audit:test", Kind: m.ScopeKindPersonal}

	g, err := p.NewGeneration("audit-gen")
	requireNoError(t, err, "%v")

	doc := recallProjectionDocument(scope, "audit-item", "audit-revision", "memory", time.Now(), 1)
	requireNoError(t, g.Index(context.Background(), doc), "%v")
	event := doc
	event.Kind = m.MemoryKindEvent
	event.ItemID = "event-item"
	event.RevisionID = "event-revision"
	event.MemoryKey = ""
	requireNoError(t, g.Index(context.Background(), event), "%v")

	requireNoError(t, g.Commit(context.Background()), "%v")

	requireNoError(t, g.Close(), "%v")

	requireNoError(t, p.ActivateGenerationForScope(context.Background(), scope, "audit-gen"), "%v")

	hits, err := p.SearchRecall(context.Background(), m.RecallRequest{Scope: scope, Query: "memory", Limit: 10})
	if err != nil || len(hits) != 2 {
		t.Fatalf("baseline hits %d err %v", len(hits), err)
	}

	kind := m.MemoryKindState

	hits,
		err = p.SearchRecall(context.Background(),
		m.RecallRequest{Scope: scope,
			Query: "memory",
			Limit: 10,
			Kind:  &kind})
	if err != nil || len(hits) != 1 {
		t.Fatalf("kind-filtered hits %d err %v, want 1", len(hits), err)
	}

	kind = m.MemoryKindEvent

	hits,
		err = p.SearchRecall(context.Background(),
		m.RecallRequest{Scope: scope,
			Query: "memory",
			Limit: 10,
			Kind:  &kind})
	if err != nil || len(hits) != 1 || hits[0].RevisionID != "event-revision" {
		t.Fatalf("event hits %+v: %v", hits, err)
	}
}
