//go:build integration

package sqlite

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	semantic "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

//nolint:funlen // At/over admission cases remain one auditable boundary table.
func TestSemanticAdmissionBoundaries(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*semantic.Generation, []semantic.CoverageRecord, []semantic.ProjectionChunk)
	}{
		{"identity512", func(_ *semantic.Generation, r []semantic.CoverageRecord, c []semantic.ProjectionChunk) {
			r[0].ItemID = strings.Repeat("i", 512)
			c[0].ItemID = r[0].ItemID
		}},
		{"scope4096 with NUL", func(g *semantic.Generation, _ []semantic.CoverageRecord, c []semantic.ProjectionChunk) {
			g.Scope.Key = "test:" + strings.Repeat("\x00", 4090) + "x"
			c[0].Scope = g.Scope
		}},
		{"dimensions1", func(g *semantic.Generation, _ []semantic.CoverageRecord, c []semantic.ProjectionChunk) {
			g.Dimensions = 1
			c[0].Dimensions = 1
			c[0].Vector = []float32{1}
		}},
		{"dimensions4096", func(g *semantic.Generation, _ []semantic.CoverageRecord, c []semantic.ProjectionChunk) {
			g.Dimensions = 4096
			c[0].Dimensions = 4096
			c[0].Vector = make([]float32, 4096)
			c[0].Vector[0] = 1
		}},
		{"escaped512x256 arrays", func(_ *semantic.Generation, _ []semantic.CoverageRecord, c []semantic.ProjectionChunk) {
			c[0].SourceIDs = maxSemanticIDs()
			c[0].ConversationIDs = maxSemanticIDs()
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, dir := openTestStore(t, true)
			g, r, c := semanticFixture()
			test.mutate(&g, r, c)
			seedSemanticSource(t, s, g)
			registerSemanticPrivacy(t, s, g, r, c)

			if _, err := s.Stage(t.Context(), g, r, c); err != nil {
				t.Fatal(err)
			}

			activateSemanticFixture(t, s, g)

			if err := s.Close(); err != nil {
				t.Fatal(err)
			}

			reopened, err := Open(t.Context(), dir, Options{EmbeddingsEnabled: true})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = reopened.Close() }()

			if _, found, err := reopened.LoadReusable(t.Context(), g.View()); err != nil || !found {
				t.Fatalf("boundary reuse=%v %v", found, err)
			}
		})
	}

	for _, test := range []struct {
		name   string
		mutate func(*semantic.Generation, []semantic.CoverageRecord, []semantic.ProjectionChunk)
	}{
		{"identity513", func(_ *semantic.Generation, _ []semantic.CoverageRecord, c []semantic.ProjectionChunk) {
			c[0].ItemID = strings.Repeat("i", 513)
		}},
		{"scope4097", func(g *semantic.Generation, _ []semantic.CoverageRecord, _ []semantic.ProjectionChunk) {
			g.Scope.Key = "test:" + strings.Repeat("s", 4092)
		}},
		{"metadata257", func(_ *semantic.Generation, _ []semantic.CoverageRecord, c []semantic.ProjectionChunk) {
			c[0].SourceIDs = append(maxSemanticIDs(), "z")
		}},
		{"metadata513", func(_ *semantic.Generation, _ []semantic.CoverageRecord, c []semantic.ProjectionChunk) {
			c[0].ConversationIDs = []string{strings.Repeat("m", 513)}
		}},
		{"dimensions4097", func(g *semantic.Generation, _ []semantic.CoverageRecord, _ []semantic.ProjectionChunk) {
			g.Dimensions = 4097
		}},
		{"dimensions0", func(g *semantic.Generation, _ []semantic.CoverageRecord, _ []semantic.ProjectionChunk) {
			g.Dimensions = 0
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, _ := openTestStore(t, true)
			g, r, c := semanticFixture()
			test.mutate(&g, r, c)

			if _, err := s.Stage(t.Context(), g, r, c); !errors.Is(err, semantic.ErrInvalid) {
				t.Fatalf("overlimit stage: %v", err)
			}
		})
	}
}

func maxSemanticIDs() []string {
	ids := make([]string, semantic.MaxMetadataIDs)
	for index := range ids {
		ids[index] = fmt.Sprintf("%03d", index) + strings.Repeat("<", 509)
	}

	return ids
}

//nolint:cyclop // At-limit release and retirement preserve the separate over-limit admission assertion.
func TestSemantic8192ChunksAnd8193RejectedBeforeClaim(t *testing.T) {
	s, _ := openTestStore(t, true)
	g, r, c := semanticFixture()
	r[0].ChunkCount = 8192

	chunks := make([]semantic.ProjectionChunk, 8192)
	for index := range chunks {
		chunks[index] = c[0]
		chunks[index].Ordinal = uint32(index)
	}

	seedSemanticSource(t, s, g)

	if state, err := s.Stage(t.Context(), g, r, chunks); err != nil || state.ChunkCount != 8192 {
		t.Fatalf("8192 stage=%#v %v", state, err)
	}

	t.Run("release at 8192 chunks", func(t *testing.T) {
		owned := g
		owned.BuildID = "release-limit"

		ownedChunks := append([]semantic.ProjectionChunk(nil), chunks...)
		for index := range ownedChunks {
			ownedChunks[index].BuildID = owned.BuildID
		}

		if state, err := s.Stage(t.Context(), owned, r, ownedChunks); err != nil || state.ChunkCount != 8192 {
			t.Fatalf("8192 owned stage=%#v %v", state, err)
		}

		result, err := s.ReleaseUnpublished(t.Context(), owned.Scope, owned.BuildID)
		if err != nil || result.Disposition != semantic.CandidateReleased {
			t.Fatalf("8192 release=%#v %v", result, err)
		}

		assertReleasedFamilies(t, s, owned.BuildID)
	})

	if err := s.Publish(t.Context(), semantic.Publication{View: g.View(), BuildID: g.BuildID}); err != nil {
		t.Fatal(err)
	}

	marker := g
	marker.BuildID = "retirement-marker"

	degraded := semantic.Degraded{Generation: marker, SafeReason: semantic.ReasonUnavailable, ExpectedActiveEpoch: 1}
	if _, err := s.PublishDegraded(t.Context(), degraded, g.BuildID); err != nil {
		t.Fatal(err)
	}

	checkSemanticRows(t, s, g.BuildID, 0)

	r[0].ChunkCount++

	chunks = append(chunks, c[0])

	g.BuildID = "over-limit"
	if _, err := s.Stage(t.Context(), g, r, chunks); !errors.Is(err, semantic.ErrCapacity) {
		t.Fatalf("8193 stage: %v", err)
	}
}

//nolint:cyclop,gocognit,funlen // Exact accounting and real reduced-target SQL grouping are checked together.
func TestSemanticSQLInputAccountingMaximumAndGrouping(t *testing.T) {
	g, r, c := semanticFixture()
	g.BuildID = strings.Repeat("b", 512)
	c[0].BuildID = g.BuildID
	c[0].ItemID = strings.Repeat("i", 512)
	c[0].RevisionID = strings.Repeat("r", 512)
	c[0].Category = memory.AtomCategory(strings.Repeat("c", 512))
	c[0].MemoryKey = memory.MemoryKey(strings.Repeat("m", 512))
	c[0].Dimensions = 4096
	c[0].Vector = make([]float32, 4096)
	c[0].Vector[0] = 1
	c[0].SourceIDs = maxSemanticIDs()
	c[0].ConversationIDs = maxSemanticIDs()
	c[0].ValidFrom = c[0].ObservedAt
	c[0].ValidUntil = c[0].ObservedAt
	c[0].ExpiresAt = c[0].ObservedAt

	size, err := (semanticMutation{chunk: &c[0]}).inputBytes(g.BuildID)
	if err != nil || size != 285434 {
		t.Fatalf("maximum chunk SQL input=%d %v", size, err)
	}

	r[0].ItemID = c[0].ItemID
	r[0].RevisionID = c[0].RevisionID

	size, err = (semanticMutation{coverage: &r[0]}).inputBytes(g.BuildID)
	if err != nil || size != 1616 {
		t.Fatalf("maximum coverage SQL input=%d %v", size, err)
	}
	// A rowid-based membership representation cannot reach the production2MiB
	// target in one admissible mutation. Exercise isolation at a reduced target.
	for _, delta := range []int{-1, 0, 1} {
		t.Run(fmt.Sprint(delta), func(t *testing.T) {
			s, _ := openTestStore(t, true)
			g, r, c := semanticFixture()
			seedSemanticSource(t, s, g)

			state, _, err := semantic.PrepareProjection(t.Context(), g, r, c)
			if err != nil {
				t.Fatal(err)
			}

			if err = s.claimSemanticBuild(t.Context(), state); err != nil {
				t.Fatal(err)
			}

			mutation := semanticMutation{coverage: &r[0]}

			size, err := mutation.inputBytes(g.BuildID)
			if err != nil {
				t.Fatal(err)
			}

			buffer := semanticStageBuffer{store: s, build: g.BuildID, target: uint64(int(size) + delta)}
			if err = buffer.add(t.Context(), mutation); err != nil {
				t.Fatal(err)
			}

			var n int
			if err = s.reader.QueryRowContext(t.Context(), "SELECT count(*) FROM semantic_coverage").
				Scan(&n); err != nil {
				t.Fatal(err)
			}

			if delta < 0 && (n != 1 || len(buffer.mutations) != 0) {
				t.Fatalf("oversize not isolated n=%d retained=%d", n, len(buffer.mutations))
			}

			if delta >= 0 && (n != 0 || len(buffer.mutations) != 1) {
				t.Fatalf("ordinary row not buffered n=%d retained=%d", n, len(buffer.mutations))
			}

			if err = buffer.flush(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

//nolint:cyclop,funlen // Exact byte construction and the one-byte rejection proof stay auditable together.
func TestSemanticExact64MiBAdmissionAndOneByteOver(t *testing.T) {
	s, dir := openTestStore(t, true)
	g, r, c := semanticFixture()
	g.Dimensions = 4096
	c[0].Dimensions = 4096
	c[0].Vector = make([]float32, 4096)
	c[0].Vector[0] = 1
	c[0].ItemID = "record-0000"
	c[0].RevisionID = c[0].ItemID

	encoded, err := semantic.EncodeChunk(c[0])
	if err != nil {
		t.Fatal(err)
	}

	rowBytes := uint64(semantic.LogicalChunkKeyBytes + len(encoded))
	// Fixed-width identities and constant-size hashes make every unpadded row equal.
	count := int((uint64(64<<20) - 1024) / rowBytes)
	coverage := make([]semantic.CoverageRecord, count)

	chunks := make([]semantic.ProjectionChunk, count)
	for index := range chunks {
		chunks[index] = c[0]
		chunks[index].ItemID = fmt.Sprintf("record-%04d", index)
		chunks[index].RevisionID = chunks[index].ItemID
		coverage[index] = r[0]
		coverage[index].ItemID = chunks[index].ItemID
		coverage[index].RevisionID = chunks[index].RevisionID
	}

	state := semantic.State{SchemaVersion: semantic.ProjectionSchemaVersion, Scope: g.Scope, BuildID: g.BuildID,
		LexicalGenerationID: g.LexicalGenerationID, LexicalWatermark: g.LexicalWatermark, ModelSpace: g.ModelSpace,
		Dimensions: g.Dimensions, Status: semantic.StatusReady, ChunkCount: uint32(count), RecordCount: uint32(count),
		CoverageHash: semantic.CoverageFingerprint(coverage), ReadyAt: g.ReadyAt}

	state, err = semantic.FinalizeEncodedBytes(state, uint64(count)*rowBytes)
	if err != nil {
		t.Fatal(err)
	}

	deficit := int(uint64(64<<20) - state.EncodedBytes)
	last := len(chunks) - 1
	// Add exactly deficit JSON bytes using independent ASCII metadata padding.
	padded := false

	for n := 1; n <= 256; n++ {
		ids := make([]string, n)
		for index := range ids {
			ids[index] = fmt.Sprintf("%03d", index)
		}

		candidate := chunks[last]
		candidate.SourceIDs = ids

		base, encodeErr := semantic.EncodeChunk(candidate)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}

		extra := deficit - (len(base) - len(encoded))
		if extra < 0 || extra > n*509 {
			continue
		}

		for index := range ids {
			addition := min(extra, 509)
			ids[index] += strings.Repeat("x", addition)
			extra -= addition
		}

		chunks[last].SourceIDs = ids
		padded = true

		break
	}

	if !padded {
		t.Fatalf("could not pad %d bytes", deficit)
	}

	over := append([]semantic.ProjectionChunk(nil), chunks...)
	over[last].SourceIDs = append([]string(nil), chunks[last].SourceIDs...)
	// The last ID retains room in this fixture; one ASCII byte adds one logical byte.
	tail := len(over[last].SourceIDs) - 1

	over[last].SourceIDs[tail] += "x"
	if _, err = s.Stage(t.Context(), g, coverage, over); !errors.Is(err, semantic.ErrCapacity) {
		t.Fatalf("64MiB+1: %v", err)
	}

	seedSemanticSource(t, s, g)

	registerSemanticPrivacy(t, s, g, coverage, chunks)

	got, err := s.Stage(t.Context(), g, coverage, chunks)
	if err != nil || got.EncodedBytes != 64<<20 {
		t.Fatalf("64MiB stage bytes=%d err=%v", got.EncodedBytes, err)
	}

	t.Run("release at exact 64MiB", func(t *testing.T) {
		owned := g
		// Equal-width generated identity preserves the independent exact-byte fixture.
		owned.BuildID = "release--build"

		ownedChunks := append([]semantic.ProjectionChunk(nil), chunks...)
		for index := range ownedChunks {
			ownedChunks[index].BuildID = owned.BuildID
		}

		if state, err := s.Stage(t.Context(), owned, coverage, ownedChunks); err != nil || state.EncodedBytes != 64<<20 {
			t.Fatalf("64MiB owned stage=%#v %v", state, err)
		}

		result, err := s.ReleaseUnpublished(t.Context(), owned.Scope, owned.BuildID)
		if err != nil || result.Disposition != semantic.CandidateReleased {
			t.Fatalf("64MiB release=%#v %v", result, err)
		}

		assertReleasedFamilies(t, s, owned.BuildID)
	})

	activateSemanticFixture(t, s, g)

	if err = s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(t.Context(), dir, Options{EmbeddingsEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()

	reused, found, err := reopened.LoadReusable(t.Context(), g.View())
	if err != nil || !found || len(reused.Chunks) != count {
		t.Fatalf("64MiB reuse count=%d found=%v err=%v", len(reused.Chunks), found, err)
	}
}
