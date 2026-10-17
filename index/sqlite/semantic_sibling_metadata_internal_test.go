package sqlite

import (
	"errors"
	"strings"
	"testing"
	"time"

	semantic "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

// Dropping any sibling metadata comparison must fail admission before a SQL
// candidate is claimed. Both chunks are otherwise individually valid, including
// the scope sequence mutation, and share valid coverage with distinct ordinals.
//
//nolint:cyclop,funlen // Keep the old ten-field assertion matrix and its positive SQL control together.
func TestSemanticStageRejectsSiblingRecordMetadataBeforeWriting(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*semantic.ProjectionChunk)
	}{
		{"consistent siblings", nil},
		{"scope sequence", func(c *semantic.ProjectionChunk) { c.ScopeChangeSeq-- }},
		{"kind", func(c *semantic.ProjectionChunk) { c.Kind = memory.MemoryKindEvent }},
		{"category", func(c *semantic.ProjectionChunk) { c.Category = memory.AtomCategory("changed") }},
		{"memory key", func(c *semantic.ProjectionChunk) { c.MemoryKey = memory.MemoryKey("changed") }},
		{"created at", func(c *semantic.ProjectionChunk) { c.CreatedAt = c.CreatedAt.Add(time.Minute) }},
		{"valid from", func(c *semantic.ProjectionChunk) {
			changed := c.ValidFrom.Add(time.Minute)
			c.ValidFrom = &changed
		}},
		{"valid until", func(c *semantic.ProjectionChunk) {
			changed := c.ValidUntil.Add(time.Minute)
			c.ValidUntil = &changed
		}},
		{"expires at", func(c *semantic.ProjectionChunk) {
			changed := c.ExpiresAt.Add(time.Minute)
			c.ExpiresAt = &changed
		}},
		{"source ids", func(c *semantic.ProjectionChunk) { c.SourceIDs = []string{"different-source"} }},
		{"conversation ids", func(c *semantic.ProjectionChunk) {
			c.ConversationIDs = []string{"different-conversation"}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, _ := openTestStore(t, true)
			g, coverage, chunks := semanticFixture()
			g.LexicalWatermark = 2
			chunks[0].LexicalWatermark = 2
			chunks[0].ScopeChangeSeq = 2
			validFrom := g.ReadyAt.Add(-time.Hour)
			validUntil := g.ReadyAt.Add(time.Hour)
			expires := g.ReadyAt.Add(2 * time.Hour)
			chunks[0].Category = memory.AtomCategory("category")
			chunks[0].MemoryKey = memory.MemoryKey("memory-key")
			chunks[0].ValidFrom = &validFrom
			chunks[0].ValidUntil = &validUntil
			chunks[0].ExpiresAt = &expires
			chunks[0].SourceIDs = []string{"source"}
			chunks[0].ConversationIDs = []string{"conversation"}
			chunks = append(chunks, chunks[0])
			chunks[1].Ordinal = 1
			chunks[1].InputHash = strings.Repeat("d", 64)
			coverage[0].ChunkCount = 2

			seedSemanticSource(t, s, g)
			registerSemanticPrivacy(t, s, g, coverage, chunks)

			if test.mutate != nil {
				test.mutate(&chunks[1])
			}

			for _, chunk := range chunks {
				if err := semantic.ValidateChunk(g, chunk); err != nil {
					t.Fatalf("sibling is not independently valid: %v", err)
				}
			}

			state, err := s.Stage(t.Context(), g, coverage, chunks)
			if test.mutate != nil {
				if !errors.Is(err, semantic.ErrInvalid) {
					t.Fatalf("mismatched sibling Stage=%#v err=%v", state, err)
				}

				assertReleasedFamilies(t, s, g.BuildID)

				return
			}

			if err != nil || state.ChunkCount != 2 || state.RecordCount != 1 {
				t.Fatalf("consistent sibling Stage=%#v err=%v", state, err)
			}

			var count int

			if err := s.reader.QueryRowContext(t.Context(),
				"SELECT count(*) FROM semantic_chunk WHERE build_id=?", g.BuildID).Scan(&count); err != nil || count != 2 {
				t.Fatalf("consistent sibling SQL rows=%d err=%v", count, err)
			}
		})
	}
}
