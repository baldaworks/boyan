//go:build integration

//nolint:lll // Fixed eight-family SQL queries keep ownership predicates auditable.
package standaloneruntime //nolint:testpackage // Exercises actual service ownership and retained SQL rows.

import (
	"context"
	"database/sql"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/baldaworks/boyan/pkg/memory"
)

type denialProvider struct {
	passages atomic.Int32
	blocked  atomic.Bool
	entered  chan struct{}
	release  chan struct{}
}

func (p *denialProvider) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if len(inputs) > 0 && strings.HasPrefix(inputs[0], "passage: ") {
		p.passages.Add(1)

		if p.blocked.Load() {
			select {
			case p.entered <- struct{}{}:
			default:
			}

			select {
			case <-p.release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}

	vectors := make([][]float32, len(inputs))
	for i := range vectors {
		vectors[i] = []float32{1, 0}
	}

	return vectors, nil
}

func denialFixture(t *testing.T) (*Runtime, Config, *denialProvider, memory.Turn) {
	t.Helper()

	p := &denialProvider{entered: make(chan struct{}, 2), release: make(chan struct{})}
	cfg := enabledLifecycleConfig(t, p)
	r, err := New(cfg)
	requireNoError(t, err, "runtime: %v")
	t.Cleanup(func() { _ = r.Stop(context.Background()) })
	first := seedSemanticTurn(t, r, "denial-first")
	startSQLComponents(t, r)
	waitSemanticState(t, r, first.Scope, "hybrid-ready")
	seedSemanticTurn(t, r, "denial-second")
	syncer := semanticSyncer{lexical: r.service.projection, manager: r.service.semantic.manager}
	requireNoError(t, syncer.Sync(t.Context(), first.Scope), "second sync: %v")
	waitSemanticState(t, r, first.Scope, "hybrid-ready")
	denialHealthy(t, r, first.Scope, 2)
	state, found, err := r.service.index.ActiveState(t.Context(), first.Scope)
	requireNoError(t, err, "published state: %v")

	if !found || state.PublicationEpoch == 0 || p.passages.Load() == 0 {
		t.Fatal("fixture lacks actual passage inference and positive publication")
	}

	db := openRuntimeSQL(t, r)
	defer func() { _ = db.Close() }()

	sqlRetainedViews(t, db, first.Scope)
	denialAssertCounts(t, db, first.Scope, "", true)

	return r, cfg, p, first
}

func denialHealthy(t *testing.T, r *Runtime, scope memory.Scope, want int) {
	t.Helper()
	response, err := r.Search(t.Context(), memory.RecallRequest{Scope: scope, Query: "Bishkek", Limit: 10})
	requireNoError(t, err, "healthy hybrid recall: %v")

	if len(response.Results) != want || response.Retrieval == nil || response.Retrieval.Effective != "hybrid" {
		t.Fatalf("healthy results=%+v, want %d", response, want)
	}
}

func denialCommand(scope memory.Scope) memory.ForgetScopeCommand {
	return memory.ForgetScopeCommand{SchemaVersion: memory.DerivedSchemaVersionV1, Scope: scope,
		RequestID: "joined-denial", ForgottenAt: time.Now().UTC()}
}

// Queries include every retained owner, never only the active publication.
func denialCounts(t *testing.T, db *sql.DB, scope memory.Scope, revision string) map[string]int {
	t.Helper()

	queries := map[string]string{
		"lexical_record":        "SELECT count(*) FROM lexical_record r JOIN lexical_build b USING(physical_build_id)",
		"lexical_fts":           "SELECT count(*) FROM lexical_fts f JOIN lexical_record r ON r.rowid=f.rowid JOIN lexical_build b USING(physical_build_id)",
		"lexical_source":        "SELECT count(*) FROM lexical_source m JOIN lexical_record r ON r.rowid=m.record_rowid JOIN lexical_build b USING(physical_build_id)",
		"lexical_conversation":  "SELECT count(*) FROM lexical_conversation m JOIN lexical_record r ON r.rowid=m.record_rowid JOIN lexical_build b USING(physical_build_id)",
		"semantic_chunk":        "SELECT count(*) FROM semantic_chunk r JOIN semantic_build b USING(build_id)",
		"semantic_coverage":     "SELECT count(*) FROM semantic_coverage r JOIN semantic_build b USING(build_id)",
		"semantic_source":       "SELECT count(*) FROM semantic_source m JOIN semantic_chunk r ON r.rowid=m.chunk_rowid JOIN semantic_build b USING(build_id)",
		"semantic_conversation": "SELECT count(*) FROM semantic_conversation m JOIN semantic_chunk r ON r.rowid=m.chunk_rowid JOIN semantic_build b USING(build_id)",
	}

	counts := make(map[string]int, len(queries))
	for family, query := range queries {
		args := []any{scope.Kind, scope.Key}

		query += " WHERE b.scope_kind=? AND b.scope_key=?"
		if revision != "" {
			query += " AND r.revision_id=?"

			args = append(args, revision)
		}

		var count int
		requireNoError(t, db.QueryRowContext(t.Context(), query, args...).Scan(&count), "count "+family+": %v")
		counts[family] = count
	}

	return counts
}

func denialAssertCounts(t *testing.T, db *sql.DB, scope memory.Scope, revision string, positive bool) {
	t.Helper()
	counts := denialCounts(t, db, scope, revision)
	t.Logf("scope=%s revision=%s positive=%t families=%v", scope.Key, revision, positive, counts)

	for family, count := range counts {
		if positive && count == 0 || !positive && count != 0 {
			t.Errorf("%s rows=%d, positive=%t", family, count, positive)
		}
	}
}
