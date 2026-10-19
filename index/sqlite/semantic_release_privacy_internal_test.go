//nolint:cyclop,funlen,lll // Real producer states and exact owner/peer postconditions belong together.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

func privacyRevokedCandidate(t *testing.T, producer string, newCanonical denialOwnerFactory) (*Store, semantic.Generation) {
	t.Helper()
	canonical := newCanonical(t)
	s, _ := openTestStore(t, true)
	g, coverage, chunks := semanticFixture()
	chunks[0].ItemID, coverage[0].ItemID = "revision", "revision"
	chunks[0].SourceIDs, chunks[0].ConversationIDs = []string{"source"}, []string{"conversation"}
	other := chunks[0]
	other.ItemID, other.RevisionID = "eligible", "eligible"
	chunks = append(chunks, other)
	otherCoverage := coverage[0]
	otherCoverage.ItemID, otherCoverage.RevisionID = "eligible", "eligible"
	coverage = append(coverage, otherCoverage)

	for _, id := range []string{"revision", "eligible"} {
		denialCanonicalRevision(t, canonical, g.Scope, id, "source")
	}

	seedPrivacySemanticSource(t, s, g)

	a, err := NewSQLiteCanonicalApplier(s, &applyReader{}, canonical)
	if err != nil {
		t.Fatal(err)
	}

	partial := producer == "zero coverage scrub" || producer == "partial chunk scrub"
	if partial {
		boundary := "BEFORE INSERT ON semantic_chunk"
		if producer == "partial chunk scrub" {
			boundary = "BEFORE UPDATE OF complete ON semantic_build"
		}

		execTest(t, s.writer, "CREATE TRIGGER fail_stage "+boundary+" BEGIN SELECT RAISE(ABORT,'stage fault'); END")
	}

	_, err = s.Stage(t.Context(), g, coverage, chunks)
	if (err != nil) != partial {
		t.Fatalf("partial=%v Stage=%v", partial, err)
	}

	if partial {
		execTest(t, s.writer, "DROP TRIGGER fail_stage")
	}
	// A healthy active winner and an independent complete empty candidate survive.
	winner := g
	winner.BuildID = "winner"

	other.BuildID = winner.BuildID
	if _, err := s.Stage(t.Context(), winner, []semantic.CoverageRecord{otherCoverage}, []semantic.ProjectionChunk{other}); err != nil {
		t.Fatal(err)
	}

	if err := s.Publish(t.Context(), semantic.Publication{View: winner.View(), BuildID: winner.BuildID}); err != nil {
		t.Fatal(err)
	}

	pending := g

	pending.BuildID = "empty-peer"
	if _, err := s.Stage(t.Context(), pending, nil, nil); err != nil {
		t.Fatal(err)
	}

	if producer == "canonical denial" {
		if err := canonical.DenyRevision(t.Context(), g.Scope, "revision", lexicalTestTime); err != nil {
			t.Fatal(err)
		}

		if err := s.ReconcileCanonicalDenials(t.Context(), g.Scope); err != nil {
			t.Fatal(err)
		}
	} else if err := a.ScrubCanonicalForget(t.Context(), g.Scope, nil, []string{"revision"}); err != nil {
		t.Fatal(err)
	}

	assertPrivacyCount(t, s, `SELECT count(*) FROM semantic_build WHERE build_id=? AND complete=0 AND status='degraded' AND reason='canonical_forget' AND epoch=?`, 1, g.BuildID, encodeUint64(0))
	assertPrivacyCount(t, s, `SELECT count(*) FROM semantic_coverage WHERE build_id=? AND revision_id='eligible'`, 1, g.BuildID)

	wantChunks := 1
	if producer == "zero coverage scrub" {
		wantChunks = 0
	}

	assertPrivacyCount(t, s, `SELECT count(*) FROM semantic_chunk WHERE build_id=?`, wantChunks, g.BuildID)

	return s, g
}

func TestSemanticReleasePrivacyRevokedProducers(t *testing.T) {
	runDenialAuthorities(t, func(t *testing.T, newCanonical denialOwnerFactory) {
		for _, producer := range []string{"chunk scrub", "zero coverage scrub", "partial chunk scrub", "canonical denial"} {
			t.Run(producer, func(t *testing.T) {
				s, g := privacyRevokedCandidate(t, producer, newCanonical)

				before, found, err := s.ActiveState(t.Context(), g.Scope)
				if err != nil || !found || before.BuildID != "winner" {
					t.Fatalf("winner=%v %v", before, err)
				}

				if err := s.Publish(t.Context(), semantic.Publication{View: g.View(), BuildID: g.BuildID, ExpectedActiveBuildID: before.BuildID, ExpectedActiveEpoch: before.PublicationEpoch}); err == nil {
					t.Fatal("revoked candidate published")
				}

				result, err := s.ReleaseUnpublished(t.Context(), g.Scope, g.BuildID)
				if err != nil || result.Disposition != semantic.CandidateReleased {
					t.Fatalf("release=%v err=%v", result, err)
				}

				assertReleasedFamilies(t, s, g.BuildID)

				after, found, err := s.ActiveState(t.Context(), g.Scope)
				if err != nil || !found || after != before {
					t.Fatalf("winner changed: %v %v", after, err)
				}

				assertPrivacyCount(t, s, `SELECT complete FROM semantic_build WHERE build_id='empty-peer'`, 1)
				assertPrivacyCount(t, s, `SELECT count(*) FROM semantic_chunk WHERE build_id='winner'`, 1)

				result, err = s.ReleaseUnpublished(t.Context(), g.Scope, g.BuildID)
				if err != nil || result.Disposition != semantic.CandidateMissing {
					t.Fatalf("repeat=%v %v", result, err)
				}
			})
		}
	})
}

func TestSemanticReleasePrivacyRevokedRejectsMalformedControl(t *testing.T) {
	runDenialAuthorities(t, func(t *testing.T, newCanonical denialOwnerFactory) {
		for _, mutation := range []string{
			"coverage_hash='bad'", "model_space='bad'", "reason='other'", "complete=1", "epoch=x'0000000000000001'",
		} {
			t.Run(mutation, func(t *testing.T) {
				s, g := privacyRevokedCandidate(t, "chunk scrub", newCanonical)
				execTest(t, s.writer, "UPDATE semantic_build SET "+mutation+" WHERE build_id=?", g.BuildID)

				result, err := s.ReleaseUnpublished(t.Context(), g.Scope, g.BuildID)
				if !errors.Is(err, semantic.ErrCorrupt) || result != (semantic.CandidateRelease{}) {
					t.Fatalf("release=%v err=%v", result, err)
				}

				assertPrivacyCount(t, s, `SELECT count(*) FROM semantic_build WHERE build_id=? AND status='degraded'`, 1, g.BuildID)
				assertPrivacyCount(t, s, `SELECT count(*) FROM semantic_chunk WHERE build_id=?`, 1, g.BuildID)
			})
		}
	})
}

func TestSemanticReleasePrivacyRevokedOwnerGuards(t *testing.T) {
	runDenialAuthorities(t, func(t *testing.T, newCanonical denialOwnerFactory) {
		s, g := privacyRevokedCandidate(t, "chunk scrub", newCanonical)
		for _, scope := range []memory.Scope{
			{Kind: g.Scope.Kind, Key: "test:other"},
			{Kind: memory.ScopeKindGroup, Key: g.Scope.Key},
		} {
			result, err := s.ReleaseUnpublished(t.Context(), scope, g.BuildID)
			if !errors.Is(err, semantic.ErrConflict) || result != (semantic.CandidateRelease{}) {
				t.Fatalf("foreign scope release=%v err=%v", result, err)
			}
		}

		s.semanticStaging.Store(g.BuildID, semanticOperationStage)
		result, err := s.ReleaseUnpublished(t.Context(), g.Scope, g.BuildID)
		s.semanticStaging.Delete(g.BuildID)

		if !errors.Is(err, semantic.ErrConflict) || result != (semantic.CandidateRelease{}) {
			t.Fatalf("live Stage release=%v err=%v", result, err)
		}
		// A malformed active epoch-zero reference must never be normalized away.
		execTest(t, s.writer, "UPDATE semantic_active SET build_id=?,epoch=?", g.BuildID, encodeUint64(0))

		result, err = s.ReleaseUnpublished(t.Context(), g.Scope, g.BuildID)
		if !errors.Is(err, semantic.ErrCorrupt) || result != (semantic.CandidateRelease{}) {
			t.Fatalf("active-zero release=%v err=%v", result, err)
		}

		assertPrivacyCount(t, s, `SELECT count(*) FROM semantic_build WHERE build_id=? AND status='degraded' AND reason='canonical_forget'`, 1, g.BuildID)
		assertPrivacyCount(t, s, `SELECT count(*) FROM semantic_chunk WHERE build_id=?`, 1, g.BuildID)
	})
}

func TestSemanticReleasePrivacyRevokedCancellationResumes(t *testing.T) {
	runDenialAuthorities(t, func(t *testing.T, newCanonical denialOwnerFactory) {
		for _, boundary := range []string{"AFTER UPDATE OF status ON semantic_build", "BEFORE DELETE ON semantic_chunk"} {
			t.Run(boundary, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()

				name := releaseSQLFunction(t, cancel)
				s, g := privacyRevokedCandidate(t, "chunk scrub", newCanonical)

				winner, found, err := s.ActiveState(t.Context(), g.Scope)
				if err != nil || !found {
					t.Fatalf("winner=%v found=%v err=%v", winner, found, err)
				}

				execTest(t, s.writer, "CREATE TRIGGER cancel_privacy_release "+boundary+" BEGIN SELECT "+name+"(); END")

				result, err := s.ReleaseUnpublished(ctx, g.Scope, g.BuildID)
				if !errors.Is(err, context.Canceled) || result != (semantic.CandidateRelease{}) {
					t.Fatalf("canceled release=%v err=%v", result, err)
				}

				status, reason := "building", ""
				if boundary == "AFTER UPDATE OF status ON semantic_build" {
					status, reason = "degraded", "canonical_forget"
				}

				assertPrivacyCount(t, s, `SELECT count(*) FROM semantic_build WHERE build_id=? AND complete=0 AND status=? AND reason=? AND epoch=?`, 1, g.BuildID, status, reason, encodeUint64(0))
				assertPrivacyCount(t, s, `SELECT count(*) FROM semantic_chunk WHERE build_id=?`, 1, g.BuildID)

				if err := s.Publish(t.Context(), semantic.Publication{View: g.View(), BuildID: g.BuildID, ExpectedActiveBuildID: winner.BuildID, ExpectedActiveEpoch: winner.PublicationEpoch}); err == nil {
					t.Fatal("interrupted release made revoked candidate publishable")
				}

				execTest(t, s.writer, "DROP TRIGGER cancel_privacy_release")

				result, err = s.ReleaseUnpublished(t.Context(), g.Scope, g.BuildID)
				if err != nil || result.Disposition != semantic.CandidateReleased {
					t.Fatalf("resumed release=%v err=%v", result, err)
				}

				assertReleasedFamilies(t, s, g.BuildID)
				assertPrivacyCount(t, s, `SELECT count(*) FROM semantic_chunk WHERE build_id='winner'`, 1)
				assertPrivacyCount(t, s, `SELECT complete FROM semantic_build WHERE build_id='empty-peer'`, 1)
			})
		}
	})
}

func TestSemanticReleaseOwnershipProbeBoundsStoredScope(t *testing.T) {
	s, dir := openTestStore(t, true)
	g, coverage, chunks := semanticFixture()
	seedSemanticSource(t, s, g)
	registerSemanticPrivacy(t, s, g, coverage, chunks)

	if _, err := s.Stage(t.Context(), g, coverage, chunks); err != nil {
		t.Fatal(err)
	}
	// Stored scope has no SQL maximum length; a corrupt value must never cross
	// the owner probe's driver boundary, even before strict state validation.
	execTest(t, s.writer, "UPDATE semantic_build SET scope_key=? WHERE build_id=?", strings.Repeat("x", 4097), g.BuildID)

	observer := &queryObserver{}
	pool := sql.OpenDB(observedConnector{poolURI(filepath.Join(dir, "search", "search.sqlite"), false, false), observer})
	pool.SetMaxOpenConns(1)

	if err := s.writer.Close(); err != nil {
		t.Fatal(err)
	}

	s.writer = pool

	result, err := s.ReleaseUnpublished(t.Context(), g.Scope, g.BuildID)
	if !errors.Is(err, semantic.ErrConflict) || result != (semantic.CandidateRelease{}) {
		t.Fatalf("oversized foreign scope release=%v err=%v", result, err)
	}

	var probe observedQuery

	for _, query := range observer.take() {
		if strings.HasPrefix(query.query, "SELECT rowid,") {
			probe = query

			break
		}
	}

	if probe.query == "" {
		t.Fatal("real release ownership probe was not observed")
	}

	rows, err := s.reader.QueryContext(t.Context(), probe.query, probe.args...)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil || len(columns) != 2 || !rows.Next() {
		t.Fatalf("ownership probe must return only rowid and match boolean: columns=%v err=%v", columns, err)
	}

	var (
		rowid   int64
		matches bool
	)
	if err := rows.Scan(&rowid, &matches); err != nil || rowid <= 0 || matches {
		t.Fatalf("bounded owner probe rowid=%d matches=%v err=%v", rowid, matches, err)
	}

	if rows.Next() || rows.Err() != nil {
		t.Fatal("unexpected extra owner or driver error")
	}

	assertPrivacyCount(t, s, `SELECT count(*) FROM semantic_chunk WHERE build_id=?`, 1, g.BuildID)
}
