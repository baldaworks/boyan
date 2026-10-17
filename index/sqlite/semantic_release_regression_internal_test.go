//nolint:cyclop,funlen,lll // Real SQLite ownership regressions retain the distinct TASK022 reviewer assertions.
package sqlite

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	semantic "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

func TestSemanticReleasePreservesCompetingWinnerAndOtherScopes(t *testing.T) {
	s, target := releaseFixture(t)

	winner := stageNamedSemantic(t, s, "review-winner")
	if err := s.Publish(t.Context(), semantic.Publication{View: winner.View(), BuildID: winner.BuildID}); err != nil {
		t.Fatal(err)
	}

	peer := stageNamedSemantic(t, s, "review-peer")

	for i, scope := range []memory.Scope{
		{Kind: memory.ScopeKindGroup, Key: target.Scope.Key},
		{Kind: target.Scope.Kind, Key: "test:unrelated"},
	} {
		g, coverage, chunks := semanticFixture()
		g.BuildID = fmt.Sprintf("review-other-%d", i)
		g.Scope = scope
		chunks[0].BuildID = g.BuildID
		chunks[0].Scope = scope

		seedSemanticSource(t, s, g)

		if _, err := s.Stage(t.Context(), g, coverage, chunks); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.Publish(t.Context(), semantic.Publication{View: target.View(), BuildID: target.BuildID}); !errors.Is(err, semantic.ErrConflict) {
		t.Fatalf("loser CAS error=%v", err)
	}

	result, err := s.ReleaseUnpublished(t.Context(), target.Scope, target.BuildID)
	if err != nil || result.Disposition != semantic.CandidateReleased {
		t.Fatalf("loser release=%#v err=%v", result, err)
	}

	assertReleasedFamilies(t, s, target.BuildID)

	for _, build := range []string{winner.BuildID, peer.BuildID, "review-other-0", "review-other-1"} {
		var count int
		if err := s.reader.QueryRowContext(t.Context(), "SELECT count(*) FROM semantic_chunk WHERE build_id=?", build).Scan(&count); err != nil || count != 1 {
			t.Fatalf("protected build=%s count=%d err=%v", build, count, err)
		}
	}
	// Release classification must not require the published source to stay current.
	execTest(t, s.writer, "UPDATE lexical_build SET watermark=?", encodeUint64(2))

	result, err = s.ReleaseUnpublished(t.Context(), winner.Scope, winner.BuildID)
	if err != nil || result.Disposition != semantic.CandidatePublished || result.Epoch != 1 {
		t.Fatalf("obsolete source active=%#v err=%v", result, err)
	}
}

func TestSemanticReleaseDurablePartialStageRepeated(t *testing.T) {
	contexts := make([]context.Context, 3)
	cancels := make([]context.CancelFunc, 3)
	names := make([]string, 3)

	for i := range contexts {
		contexts[i], cancels[i] = context.WithCancel(t.Context())
		defer cancels[i]()

		names[i] = releaseSQLFunction(t, cancels[i])
	}

	s, _ := openTestStore(t, true)
	g, coverage, fixtureChunks := semanticFixture()
	seedSemanticSource(t, s, g)

	coverage[0].ChunkCount = 130

	for iteration := range 3 {
		ctx, cancel := contexts[iteration], cancels[iteration]
		name := names[iteration]
		g.BuildID = fmt.Sprintf("review-partial-%d", iteration)

		chunks := make([]semantic.ProjectionChunk, 130)
		for i := range chunks {
			chunks[i] = fixtureChunks[0]
			chunks[i].BuildID = g.BuildID
			chunks[i].Ordinal = uint32(i)
			chunks[i].SourceIDs = []string{"source"}
			chunks[i].ConversationIDs = []string{"conversation"}
		}

		registerSemanticPrivacy(t, s, g, coverage, chunks)

		execTest(t, s.writer, "CREATE TRIGGER review_cancel_stage AFTER INSERT ON semantic_chunk WHEN NEW.ordinal=129 BEGIN SELECT "+name+"(); END")
		_, err := s.Stage(ctx, g, coverage, chunks)

		cancel()

		if !errors.Is(err, context.Canceled) {
			t.Fatalf("partial stage err=%v", err)
		}

		var count int
		if err := s.reader.QueryRowContext(t.Context(), "SELECT count(*) FROM semantic_chunk WHERE build_id=?", g.BuildID).Scan(&count); err != nil || count != 128 {
			t.Fatalf("partial page count=%d err=%v", count, err)
		}

		execTest(t, s.writer, "DROP TRIGGER review_cancel_stage")

		result, err := s.ReleaseUnpublished(t.Context(), g.Scope, g.BuildID)
		if err != nil || result.Disposition != semantic.CandidateReleased {
			t.Fatalf("partial release=%#v err=%v", result, err)
		}

		assertReleasedFamilies(t, s, g.BuildID)
	}

	for _, table := range []string{"semantic_build", "semantic_coverage", "semantic_chunk", "semantic_source", "semantic_conversation"} {
		var count int
		if err := s.reader.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("accumulated %s rows=%d err=%v", table, count, err)
		}
	}
}

func TestSemanticReleaseDeadlineWaitingScope(t *testing.T) {
	s, g := releaseFixture(t)

	unlock, err := s.lockScope(t.Context(), g.Scope)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()

	result, err := s.ReleaseUnpublished(ctx, g.Scope, g.BuildID)

	unlock()

	if !errors.Is(err, context.DeadlineExceeded) || result != (semantic.CandidateRelease{}) {
		t.Fatalf("deadline result=%#v err=%v", result, err)
	}

	if _, live := s.semanticStaging.Load(g.BuildID); live {
		t.Fatal("deadline leaked reservation")
	}

	result, err = s.ReleaseUnpublished(t.Context(), g.Scope, g.BuildID)
	if err != nil || result.Disposition != semantic.CandidateReleased {
		t.Fatalf("deadline resume=%#v err=%v", result, err)
	}
}
