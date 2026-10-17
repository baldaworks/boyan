//nolint:cyclop,lll // Real SQL state-machine cases assert the exact candidate and all owned families.
package sqlite

import (
	"context"
	"errors"
	"strings"
	"testing"

	semantic "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

func releaseFixture(t *testing.T) (*Store, semantic.Generation) {
	t.Helper()
	s, _ := openTestStore(t, true)
	g, coverage, chunks := semanticFixture()
	chunks[0].SourceIDs = []string{"source"}
	chunks[0].ConversationIDs = []string{"conversation"}

	seedSemanticSource(t, s, g)

	if _, err := s.Stage(t.Context(), g, coverage, chunks); err != nil {
		t.Fatal(err)
	}

	return s, g
}

func assertReleasedFamilies(t *testing.T, s *Store, build string) {
	t.Helper()

	for _, table := range []string{"semantic_build", "semantic_coverage", "semantic_chunk"} {
		var count int
		if err := s.reader.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table+" WHERE build_id=?", build).Scan(&count); err != nil || count != 0 {
			t.Fatalf("owned %s rows=%d err=%v", table, count, err)
		}
	}

	for _, table := range []string{"semantic_source", "semantic_conversation"} {
		var dangling int
		if err := s.reader.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table+" m WHERE NOT EXISTS(SELECT 1 FROM semantic_chunk c WHERE c.rowid=m.chunk_rowid)").Scan(&dangling); err != nil || dangling != 0 {
			t.Fatalf("dangling %s rows=%d err=%v", table, dangling, err)
		}
	}
}

func TestSemanticReleaseExactCompleteAndRepeated(t *testing.T) {
	s, g := releaseFixture(t)
	peer, coverage, chunks := semanticFixture()
	peer.BuildID = "paused-complete-peer"

	chunks[0].BuildID = peer.BuildID
	if _, err := s.Stage(t.Context(), peer, coverage, chunks); err != nil {
		t.Fatal(err)
	}

	result, err := s.ReleaseUnpublished(t.Context(), g.Scope, g.BuildID)
	if err != nil || result.Disposition != semantic.CandidateReleased || result.Epoch != 0 {
		t.Fatalf("release=%#v err=%v", result, err)
	}

	assertReleasedFamilies(t, s, g.BuildID)

	var complete int
	if err := s.reader.QueryRowContext(t.Context(), "SELECT complete FROM semantic_build WHERE build_id=?", peer.BuildID).Scan(&complete); err != nil || complete != 1 {
		t.Fatalf("peer changed: complete=%d err=%v", complete, err)
	}

	for range 2 {
		result, err = s.ReleaseUnpublished(t.Context(), g.Scope, g.BuildID)
		if err != nil || result.Disposition != semantic.CandidateMissing || result.Epoch != 0 {
			t.Fatalf("repeat=%#v err=%v", result, err)
		}
	}
}

func TestSemanticReleasePublishedAndConflictingControl(t *testing.T) {
	for _, test := range []struct {
		name     string
		mutation string
		active   bool
		want     error
	}{
		{"published", "", true, nil},
		{"nonactive positive", "UPDATE semantic_build SET epoch=x'0000000000000001'", false, semantic.ErrConflict},
		{"active zero", "UPDATE semantic_build SET epoch=x'0000000000000000'; UPDATE semantic_active SET epoch=x'0000000000000000'", true, semantic.ErrCorrupt},
		{"inconsistent active", "UPDATE semantic_active SET epoch=x'0000000000000002'", true, semantic.ErrCorrupt},
		{"incomplete active", "UPDATE semantic_build SET complete=0", true, semantic.ErrCorrupt},
		{"bad control", "UPDATE semantic_build SET coverage_hash='bad'", false, semantic.ErrCorrupt},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, g := releaseFixture(t)
			if test.active {
				activateSemanticFixture(t, s, g)
			}

			if test.mutation != "" {
				execTest(t, s.writer, "PRAGMA foreign_keys=OFF")
				execTest(t, s.writer, test.mutation)
				execTest(t, s.writer, "PRAGMA foreign_keys=ON")
			}

			result, err := s.ReleaseUnpublished(t.Context(), g.Scope, g.BuildID)
			if test.want != nil {
				if !errors.Is(err, test.want) || result != (semantic.CandidateRelease{}) {
					t.Fatalf("result=%#v err=%v", result, err)
				}
			} else if err != nil || result.Disposition != semantic.CandidatePublished || result.Epoch != 1 {
				t.Fatalf("published=%#v err=%v", result, err)
			}

			var count int
			if err := s.reader.QueryRowContext(t.Context(), "SELECT count(*) FROM semantic_chunk WHERE build_id=?", g.BuildID).Scan(&count); err != nil || count != 1 {
				t.Fatalf("target mutated count=%d err=%v", count, err)
			}
		})
	}
}

func TestSemanticReleaseInvalidWrongScopeAndCancellation(t *testing.T) {
	s, g := releaseFixture(t)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()

	for _, test := range []struct {
		name  string
		ctx   context.Context
		scope memory.Scope
		build string
		want  error
	}{
		{"nil context", nil, g.Scope, g.BuildID, semantic.ErrInvalid},
		{"invalid scope", t.Context(), memory.Scope{}, g.BuildID, semantic.ErrInvalid},
		{"empty build", t.Context(), g.Scope, "", semantic.ErrInvalid},
		{"nul build", t.Context(), g.Scope, "bad\x00build", semantic.ErrInvalid},
		{"long build", t.Context(), g.Scope, strings.Repeat("x", 513), semantic.ErrInvalid},
		{"wrong key", t.Context(), memory.Scope{Kind: g.Scope.Kind, Key: "test:other"}, g.BuildID, semantic.ErrConflict},
		{"wrong kind", t.Context(), memory.Scope{Kind: memory.ScopeKindGroup, Key: g.Scope.Key}, g.BuildID, semantic.ErrConflict},
		{"canceled", canceled, g.Scope, g.BuildID, context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := s.ReleaseUnpublished(test.ctx, test.scope, test.build)
			if !errors.Is(err, test.want) || result != (semantic.CandidateRelease{}) {
				t.Fatalf("release=%#v err=%v", result, err)
			}
		})
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := s.ReleaseUnpublished(t.Context(), g.Scope, g.BuildID); !errors.Is(err, semantic.ErrClosed) {
		t.Fatalf("closed=%v", err)
	}
}

func TestSemanticReleaseEmptyAndZeroChunkCoverage(t *testing.T) {
	for _, zeroCoverage := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "zero chunk coverage"}[zeroCoverage], func(t *testing.T) {
			s, _ := openTestStore(t, true)
			g, coverage, _ := semanticFixture()
			seedSemanticSource(t, s, g)

			if _, err := s.Stage(t.Context(), g, nil, nil); err != nil {
				t.Fatal(err)
			}
			// A scrubbed or interrupted candidate may retain zero-chunk coverage.
			// Portable Stage admission deliberately remains unchanged.
			if zeroCoverage {
				coverage[0].ChunkCount = 0
				execTest(t, s.writer, "INSERT INTO semantic_coverage VALUES(?,?,?,?,?,0)", g.BuildID, coverage[0].ItemID, coverage[0].RevisionID, encodeUint64(1), coverage[0].ContentHash)
				execTest(t, s.writer, "UPDATE semantic_build SET revision_count=1,coverage_hash=? WHERE build_id=?", semantic.CoverageFingerprint(coverage), g.BuildID)
			}

			result, err := s.ReleaseUnpublished(t.Context(), g.Scope, g.BuildID)
			if err != nil || result.Disposition != semantic.CandidateReleased {
				t.Fatalf("release=%#v err=%v", result, err)
			}

			assertReleasedFamilies(t, s, g.BuildID)
		})
	}
}
