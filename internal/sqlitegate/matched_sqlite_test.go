//nolint:cyclop,funlen,lll // Exact production-backed fixture fields stay directly comparable to the old adapter template.
package sqlitegate_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	search "github.com/baldaworks/boyan/index/sqlite"
	core "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/internal/semanticprojection"
	m "github.com/baldaworks/boyan/pkg/memory"
)

type matchedSQLite struct {
	c          *matchedCorpus
	store      *search.Store
	applier    *search.SQLiteCanonicalApplier
	h          *semanticprojection.Hybrid
	provider   *matchedProvider
	generation string
	reads      *matchedSQLReads
}

type matchedSQLReads struct {
	*search.Store

	physical, snapshots, lexical, dense uint64
}

func (r *matchedSQLReads) LoadActivePhysicalProjectionManifest(ctx context.Context, scope m.Scope, id string) (m.ProjectionManifest, bool, error) {
	r.physical++

	return r.Store.LoadActivePhysicalProjectionManifest(ctx, scope, id)
}
func (r *matchedSQLReads) OpenSearchSnapshot(ctx context.Context, scope m.Scope) (core.SearchSnapshot, error) {
	r.snapshots++

	snapshot, err := r.Store.OpenSearchSnapshot(ctx, scope)
	if err != nil {
		return nil, err
	}

	return &matchedSnapshot{SearchSnapshot: snapshot, reads: r}, nil
}

type matchedSnapshot struct {
	core.SearchSnapshot

	reads *matchedSQLReads
}

func (s *matchedSnapshot) SearchLexical(ctx context.Context, req m.RecallRequest, depth int) ([]m.RecallProjectionHit, error) {
	s.reads.lexical++

	return s.SearchSnapshot.SearchLexical(ctx, req, depth)
}
func (s *matchedSnapshot) SearchSemantic(ctx context.Context, req m.RecallRequest, v [][]float32, depth int) ([]core.DenseMatch, core.ScanCounts, error) {
	s.reads.dense++

	return s.SearchSnapshot.SearchSemantic(ctx, req, v, depth)
}
func (b *matchedSQLite) counters() map[string]uint64 {
	return map[string]uint64{"provider": uint64(b.provider.calls), "snapshot": b.reads.snapshots, "physical": b.reads.physical, "lexical_channel": b.reads.lexical, "dense_channel": b.reads.dense, "canonical_scope": b.c.reader.scopeReads, "canonical_manifest": b.c.reader.manifestReads, "hydrate_calls": b.c.reader.loads, "hydrate_ids": b.c.reader.ids}
}

func matchedEngine() string { return "final" }
func (b *matchedSQLite) dense(ctx context.Context) ([]core.DenseMatch, error) {
	snapshot, err := b.store.OpenSearchSnapshot(ctx, b.c.scope)
	if err != nil {
		return nil, err
	}
	defer snapshot.Close()

	vectors, _ := b.provider.Embed(ctx, []string{"0", "1", "2", "3"})

	hits, counts, err := snapshot.SearchSemantic(ctx, m.RecallRequest{Scope: b.c.scope, Query: matchedQuery(), Limit: 25, AsOf: &matchedAt}, vectors, 100)
	if err == nil && counts.ScannedChunks != 8192 {
		return nil, fmt.Errorf("scanned=%d", counts.ScannedChunks)
	}

	return hits, err
}

func openMatchedBackend(ctx context.Context, c *matchedCorpus) (matchedBackend, error) {
	b := &matchedSQLite{c: c, provider: &matchedProvider{}}
	if err := b.open(ctx); err != nil {
		return nil, err
	}

	return b, nil
}
func (b *matchedSQLite) open(ctx context.Context) error {
	dir := b.c.root
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	s, err := search.Open(ctx, dir, search.Options{EmbeddingsEnabled: true})
	if err != nil {
		return err
	}

	b.store = s
	if b.reads == nil {
		b.reads = &matchedSQLReads{}
	}

	b.reads.Store = s

	b.applier, err = search.NewSQLiteCanonicalApplier(s, b.c.reader, b.c.reader)
	if err != nil {
		return err
	}

	b.applier.ConfigureProjectionRetention(b.c.canonical, "sqlite")
	b.h, err = semanticprojection.NewHybrid(semanticprojection.HybridDependencies{Canonical: b.c.reader, Manifests: b.c.reader, Reader: b.reads, Lexical: s, Provider: b.provider, ModelSpace: matchedSpace()})

	return err
}
func (b *matchedSQLite) lexical() m.RecallProjection { return b.store }
func (b *matchedSQLite) hybrid() m.RecallProjection  { return b.h }
func (b *matchedSQLite) rebuild(ctx context.Context, generation string) error {
	if err := b.applier.BeginProjectionRebuild(ctx, b.c.scope, generation); err != nil {
		return err
	}

	for offset := 0; offset < len(b.c.revisions); offset += 128 {
		if err := b.applier.Apply(ctx, b.c.scope, generation, nil, b.c.revisions[offset:min(offset+128, len(b.c.revisions))]); err != nil {
			return err
		}
	}

	if err := b.publish(ctx, generation); err != nil {
		return err
	}

	b.generation = generation

	return nil
}
func (b *matchedSQLite) publish(ctx context.Context, generation string) error {
	manifest := m.ProjectionManifest{Scope: b.c.scope, ProjectionID: "sqlite", GenerationID: generation, Status: m.ProjectionGenerationDirty, Watermark: b.c.seq, UpdatedAt: matchedAt}
	if err := b.c.canonical.MarkProjectionDirty(ctx, manifest); err != nil {
		return err
	}

	if err := b.applier.CommitProjectionManifest(ctx, manifest); err != nil {
		return err
	}

	if err := b.c.canonical.AdvanceProjectionWatermark(ctx, manifest, b.c.seq, matchedAt); err != nil {
		return err
	}

	if err := b.c.canonical.ActivateProjectionGeneration(ctx, manifest, matchedAt); err != nil {
		return err
	}

	return b.applier.Activate(ctx, b.c.scope, generation)
}
func (b *matchedSQLite) copy(ctx context.Context) error {
	if err := b.applier.Apply(ctx, b.c.scope, b.generation, nil, nil); err != nil {
		return err
	}

	return b.publish(ctx, b.generation)
}
func (b *matchedSQLite) reopen(ctx context.Context) error {
	if err := b.close(); err != nil {
		return err
	}

	return b.open(ctx)
}
func (b *matchedSQLite) close() error {
	if b.store == nil {
		return nil
	}

	err := b.store.Close()
	b.store = nil

	return err
}
func (b *matchedSQLite) bytes() map[string]int64 {
	return projectionFootprint(filepath.Join(b.c.root, "search"))
}

func (b *matchedSQLite) semantic(ctx context.Context, status string) error {
	space, err := core.SpaceFingerprint(matchedSpace())
	if err != nil {
		return err
	}

	g := core.Generation{Scope: b.c.scope, BuildID: "semantic-profile-" + status, LexicalGenerationID: b.generation, LexicalWatermark: b.c.seq, ModelSpace: space, Dimensions: 768, ReadyAt: matchedAt}
	if status == "missing" {
		return nil
	}

	if status == "degraded" {
		state, found, err := b.store.ActiveState(ctx, b.c.scope)
		if err != nil {
			return err
		}

		if found && state.Status == core.StatusDegraded {
			return nil
		}

		_, err = b.store.PublishDegraded(ctx, core.Degraded{Generation: g, SafeReason: core.ReasonUnavailable}, "")

		return err
	}

	coverage := make([]core.CoverageRecord, len(b.c.records))
	chunks := make([]core.ProjectionChunk, 0, 8192)

	for i, r := range b.c.records {
		hash := sha256.Sum256([]byte(r.Text))
		digest := hex.EncodeToString(hash[:])

		n := 1
		if i == 0 && b.c.name == "semantic8192" {
			n = 128
		}

		coverage[i] = core.CoverageRecord{ItemID: r.ItemID, RevisionID: r.RevisionID, Revision: 1, ContentHash: digest, ChunkCount: uint32(n)}
		for ordinal := range n {
			index := len(chunks)
			v := make([]float32, 768)
			v[index%768] = 1
			chunks = append(chunks, core.ProjectionChunk{Scope: r.Scope, BuildID: g.BuildID, LexicalGenerationID: g.LexicalGenerationID, LexicalWatermark: g.LexicalWatermark, ModelSpace: space, Dimensions: 768, ItemID: r.ItemID, RevisionID: r.RevisionID, Revision: 1, Ordinal: uint32(ordinal), ContentHash: digest, InputHash: digest, ScopeChangeSeq: b.c.seq, Kind: r.Kind, Category: *r.Category, MemoryKey: r.MemoryKey, CreatedAt: r.CreatedAt, ObservedAt: &matchedAt, SourceIDs: r.SourceIDs, ConversationIDs: r.ConversationIDs, Vector: v})
		}
	}

	expected := len(b.c.records)
	if b.c.name == "semantic8192" {
		expected += 127
	}

	if len(chunks) != expected {
		return fmt.Errorf("dense chunks=%d", len(chunks))
	}

	start := time.Now()

	state, err := b.store.Stage(ctx, g, coverage, chunks)
	if err != nil {
		return err
	}

	if state.ChunkCount != uint32(expected) || state.RecordCount != uint32(len(b.c.records)) {
		return fmt.Errorf("coverage count mismatch")
	}

	fmt.Printf("STAGE actual_sqlite ns=%d coverage=%d chunks=%d logical_bytes=%d authority_revision=%d authority_source=%d structures=%d\n", time.Since(start).Nanoseconds(), state.RecordCount, state.ChunkCount, state.EncodedBytes, b.c.reader.revisionDenials, b.c.reader.sourceDenials, b.c.reader.structures)

	return b.store.Publish(ctx, core.Publication{View: g.View(), BuildID: g.BuildID})
}
