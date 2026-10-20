//go:build integration

package semanticprojection //nolint:testpackage // Retains neutral orchestration controls against real SQLite.

import (
	"context"
	"database/sql"
	"encoding/binary"
	"os"
	"path/filepath"
	"sync"
	"testing"

	sqlitestore "github.com/baldaworks/boyan/index/sqlite"
	semanticcore "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

// neutralSemanticStore keeps only fixture plumbing: the scheduler's synthetic
// canonical reader supplies known identities independently of Stage. An empty
// sealed lexical tuple supplies actual SQL publication authority; all semantic
// persistence, CAS, retirement and owner lifecycle execute the real adapter.
type neutralSemanticStore struct {
	*sqlitestore.Store

	mu          sync.Mutex
	applier     *sqlitestore.SQLiteCanonicalApplier
	checkpoints *neutralCheckpoints
	privacy     *neutralFixturePrivacy
	db          *sql.DB
}

func openSemanticStore(t *testing.T, canonical *fakeCanonical) *neutralSemanticStore {
	t.Helper()

	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	store, err := sqlitestore.Open(t.Context(), dir, sqlitestore.Options{EmbeddingsEnabled: true})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})

	privacy := &neutralFixturePrivacy{sqliteFixturePrivacy: &sqliteFixturePrivacy{canonical: canonical,
		revisions: make(map[string]memory.MemoryRevision), sources: make(map[string]bool)}}
	privacy.registerKnown()

	applier, err := sqlitestore.NewSQLiteCanonicalApplier(store, canonical, privacy)
	if err != nil {
		t.Fatal(err)
	}

	checkpoints := &neutralCheckpoints{manifests: make(map[memory.Scope]memory.ProjectionManifest)}
	applier.ConfigureProjectionRetention(checkpoints, "sqlite")

	db, err := sql.Open("sqlite", filepath.Join(dir, "search", "search.sqlite"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = db.Close() })

	return &neutralSemanticStore{Store: store, applier: applier, checkpoints: checkpoints, privacy: privacy, db: db}
}

func (s *neutralSemanticStore) Stage(ctx context.Context, generation semanticcore.Generation,
	coverage []semanticcore.CoverageRecord, chunks []semanticcore.ProjectionChunk) (semanticcore.State, error) {
	if err := s.prepareLexical(ctx, generation); err != nil {
		return semanticcore.State{}, err
	}

	return s.Store.Stage(ctx, generation, coverage, chunks)
}

func (s *neutralSemanticStore) PublishDegraded(ctx context.Context, marker semanticcore.Degraded,
	expected string) (semanticcore.State, error) {
	if err := s.prepareLexical(ctx, marker.Generation); err != nil {
		return semanticcore.State{}, err
	}

	return s.Store.PublishDegraded(ctx, marker, expected)
}

func (s *neutralSemanticStore) prepareLexical(ctx context.Context, generation semanticcore.Generation) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Register the independently known canonical population once per fixture
	// write attempt, retaining history. Per-ID authority callbacks stay O(1)
	// instead of re-importing all8192 records for each coverage/chunk decision.
	s.privacy.registerKnown()

	manifest := activeManifest(generation.Scope, generation.LexicalGenerationID, generation.LexicalWatermark)

	current, found, err := s.Store.LoadActivePhysicalProjectionManifest(ctx, generation.Scope, "sqlite")
	if err != nil {
		return err
	}

	if found && current.GenerationID == manifest.GenerationID && current.Watermark == manifest.Watermark {
		return nil
	}

	s.checkpoints.set(manifest)

	if err := s.applier.BeginProjectionRebuild(ctx, generation.Scope, manifest.GenerationID); err != nil {
		return err
	}

	if err := s.applier.CommitProjectionManifest(ctx, manifest); err != nil {
		return err
	}

	return s.applier.Activate(ctx, generation.Scope, manifest.GenerationID)
}

func storedChunkSequence(t *testing.T, store *neutralSemanticStore, revision string) uint64 {
	t.Helper()

	var value []byte

	const query = "SELECT change_seq FROM semantic_chunk WHERE revision_id=?"
	if err := store.db.QueryRowContext(t.Context(), query, revision).Scan(&value); err != nil {
		t.Fatal(err)
	}

	if len(value) != 8 {
		t.Fatalf("invalid stored scope sequence %x", value)
	}

	return binary.BigEndian.Uint64(value)
}

type neutralCheckpoints struct {
	memory.ProjectionCheckpointStore

	mu        sync.Mutex
	manifests map[memory.Scope]memory.ProjectionManifest
}

func (c *neutralCheckpoints) LoadActiveProjectionManifest(ctx context.Context, scope memory.Scope,
	projectionID string) (memory.ProjectionManifest, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	manifest, found := c.manifests[scope]

	return manifest, found && projectionID == "sqlite", ctx.Err()
}

func (c *neutralCheckpoints) LoadProjectionManifest(ctx context.Context, scope memory.Scope,
	projectionID, generation string) (memory.ProjectionManifest, bool, error) {
	manifest, found, err := c.LoadActiveProjectionManifest(ctx, scope, projectionID)

	return manifest, found && manifest.GenerationID == generation, err
}

func (c *neutralCheckpoints) set(manifest memory.ProjectionManifest) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.manifests[manifest.Scope] = manifest
}

// neutralFixturePrivacy uses the accepted known-history representation, with
// explicit population registration outside production admission. It does not
// change the shared mutable fixture used by the existing SQL/Hybrid matrices.
type neutralFixturePrivacy struct{ *sqliteFixturePrivacy }

func (p *neutralFixturePrivacy) IsRevisionDenied(ctx context.Context, scope memory.Scope,
	id string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, known := p.revisions[id]; scope != p.canonical.scope || !known {
		return false, memory.PermanentError(memory.CodeNotFound, "unknown SQL fixture revision", nil)
	}

	return false, ctx.Err()
}

func (p *neutralFixturePrivacy) IsSourceDenied(ctx context.Context, scope memory.Scope,
	id string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if scope != p.canonical.scope || !p.sources[id] {
		return false, memory.PermanentError(memory.CodeNotFound, "unknown SQL fixture source", nil)
	}

	return false, ctx.Err()
}

func (p *neutralFixturePrivacy) LoadCanonicalRevisions(ctx context.Context,
	request memory.CanonicalRevisionReadRequest) ([]memory.MemoryRevision, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	var result []memory.MemoryRevision

	for _, id := range request.RevisionIDs {
		revision, known := p.revisions[id]
		if request.Scope != p.canonical.scope || !known {
			return nil, memory.PermanentError(memory.CodeNotFound, "unknown SQL fixture structure", nil)
		}

		result = append(result, revision)
	}

	return result, ctx.Err()
}

func (p *neutralFixturePrivacy) registerKnown() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.importKnown()
}
