//nolint:cyclop,gocognit,lll // Real SQL failures cover each durable release boundary and exact family rollback.
package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"

	semantic "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
	sqlitedriver "modernc.org/sqlite"
)

func releaseSQLFunction(t *testing.T, action func()) string {
	t.Helper()

	name := fmt.Sprintf("release_test_%d", semanticCancelSequence.Add(1))
	if err := sqlitedriver.RegisterScalarFunction(name, 0, func(*sqlitedriver.FunctionContext, []driver.Value) (driver.Value, error) {
		action()

		return int64(0), nil
	}); err != nil {
		t.Fatal(err)
	}

	return name
}

func TestSemanticReleaseSQLFailureResumesEveryFamily(t *testing.T) {
	for _, test := range []struct {
		name     string
		trigger  string
		complete int
	}{
		{"before revocation", "BEFORE UPDATE OF complete ON semantic_build", 1},
		{"chunk page", "BEFORE DELETE ON semantic_chunk", 0},
		{"source cascade", "BEFORE DELETE ON semantic_source", 0},
		{"conversation cascade", "BEFORE DELETE ON semantic_conversation", 0},
		{"coverage page", "BEFORE DELETE ON semantic_coverage", 0},
		{"control removal", "BEFORE DELETE ON semantic_build", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, g := releaseFixture(t)
			peer := stageNamedSemantic(t, s, "protected-peer")
			execTest(t, s.writer, "CREATE TRIGGER fail_release "+test.trigger+" BEGIN SELECT RAISE(ABORT,'secret SQL provider payload'); END")

			result, err := s.ReleaseUnpublished(t.Context(), g.Scope, g.BuildID)
			if err == nil || result != (semantic.CandidateRelease{}) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("unsafe result=%#v err=%v", result, err)
			}

			var classified *memory.Error
			if !errors.As(err, &classified) || classified.Code != memory.CodeStoreFailure {
				t.Fatalf("unclassified SQL failure=%v", err)
			}

			var complete int
			if err := s.reader.QueryRowContext(t.Context(), "SELECT complete FROM semantic_build WHERE build_id=?", g.BuildID).Scan(&complete); err != nil || complete != test.complete {
				t.Fatalf("revocation=%d err=%v", complete, err)
			}

			if _, live := s.semanticStaging.Load(g.BuildID); live {
				t.Fatal("release reservation leaked")
			}

			if test.complete == 0 {
				if err := s.Publish(t.Context(), semantic.Publication{View: g.View(), BuildID: g.BuildID}); !errors.Is(err, semantic.ErrConflict) {
					t.Fatalf("revoked build published=%v", err)
				}
			}

			execTest(t, s.writer, "DROP TRIGGER fail_release")

			result, err = s.ReleaseUnpublished(t.Context(), g.Scope, g.BuildID)
			if err != nil || result.Disposition != semantic.CandidateReleased {
				t.Fatalf("resume=%#v err=%v", result, err)
			}

			assertReleasedFamilies(t, s, g.BuildID)

			var peerComplete int
			if err := s.reader.QueryRowContext(t.Context(), "SELECT complete FROM semantic_build WHERE build_id=?", peer.BuildID).Scan(&peerComplete); err != nil || peerComplete != 1 {
				t.Fatalf("peer=%d err=%v", peerComplete, err)
			}
		})
	}
}

func TestSemanticReleaseAfterActualStageFailure(t *testing.T) {
	for _, boundary := range []string{"BEFORE INSERT ON semantic_build", "BEFORE INSERT ON semantic_coverage", "BEFORE INSERT ON semantic_chunk", "BEFORE UPDATE OF complete ON semantic_build"} {
		t.Run(boundary, func(t *testing.T) {
			s, _ := openTestStore(t, true)
			g, coverage, chunks := semanticFixture()
			seedSemanticSource(t, s, g)
			execTest(t, s.writer, "CREATE TRIGGER fail_stage "+boundary+" BEGIN SELECT RAISE(ABORT,'stage fault'); END")

			if _, err := s.Stage(t.Context(), g, coverage, chunks); err == nil {
				t.Fatal("fault did not fail Stage")
			}

			execTest(t, s.writer, "DROP TRIGGER fail_stage")
			result, err := s.ReleaseUnpublished(t.Context(), g.Scope, g.BuildID)

			want := semantic.CandidateReleased
			if strings.Contains(boundary, "INSERT ON semantic_build") {
				want = semantic.CandidateMissing
			}

			if err != nil || result.Disposition != want {
				t.Fatalf("partial Stage result=%#v want=%s err=%v", result, want, err)
			}

			assertReleasedFamilies(t, s, g.BuildID)
		})
	}
}

func TestSemanticReleaseCancellationRollsBackCurrentPage(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	name := releaseSQLFunction(t, cancel)
	s, _ := openTestStore(t, true)
	g, coverage, c := semanticFixture()
	coverage[0].ChunkCount = 257

	chunks := make([]semantic.ProjectionChunk, 257)
	for i := range chunks {
		chunks[i] = c[0]
		chunks[i].Ordinal = uint32(i)
		chunks[i].SourceIDs = []string{"source"}
		chunks[i].ConversationIDs = []string{"conversation"}
	}

	seedSemanticSource(t, s, g)

	registerSemanticPrivacy(t, s, g, coverage, chunks)

	if _, err := s.Stage(t.Context(), g, coverage, chunks); err != nil {
		t.Fatal(err)
	}

	execTest(t, s.writer, "CREATE TRIGGER cancel_release BEFORE DELETE ON semantic_chunk WHEN OLD.ordinal=128 BEGIN SELECT "+name+"(); END")

	result, err := s.ReleaseUnpublished(ctx, g.Scope, g.BuildID)
	if !errors.Is(err, context.Canceled) || result != (semantic.CandidateRelease{}) {
		t.Fatalf("canceled result=%#v err=%v", result, err)
	}

	for _, table := range []string{"semantic_chunk", "semantic_source", "semantic_conversation"} {
		var count int
		if err := s.reader.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 129 {
			t.Fatalf("page rollback %s=%d err=%v", table, count, err)
		}
	}

	var complete int
	if err := s.reader.QueryRowContext(t.Context(), "SELECT complete FROM semantic_build WHERE build_id=?", g.BuildID).Scan(&complete); err != nil || complete != 0 {
		t.Fatalf("revocation rolled back=%d err=%v", complete, err)
	}

	execTest(t, s.writer, "DROP TRIGGER cancel_release")

	if err := s.CollectGarbage(t.Context()); err != nil {
		t.Fatal(err)
	}

	assertReleasedFamilies(t, s, g.BuildID)
}

func TestSemanticReleaseCancellationAtRevocationAndControl(t *testing.T) {
	for _, boundary := range []string{"AFTER UPDATE OF complete ON semantic_build", "BEFORE DELETE ON semantic_build"} {
		t.Run(boundary, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			name := releaseSQLFunction(t, cancel)
			s, g := releaseFixture(t)
			execTest(t, s.writer, "CREATE TRIGGER cancel_boundary "+boundary+" BEGIN SELECT "+name+"(); END")

			result, err := s.ReleaseUnpublished(ctx, g.Scope, g.BuildID)
			if !errors.Is(err, context.Canceled) || result != (semantic.CandidateRelease{}) {
				t.Fatalf("result=%#v err=%v", result, err)
			}

			var complete int
			if err := s.reader.QueryRowContext(t.Context(), "SELECT complete FROM semantic_build WHERE build_id=?", g.BuildID).Scan(&complete); err != nil {
				t.Fatal(err)
			}

			want := 0
			if strings.HasPrefix(boundary, "AFTER UPDATE") {
				want = 1
			}

			if complete != want {
				t.Fatalf("complete=%d want=%d", complete, want)
			}

			execTest(t, s.writer, "DROP TRIGGER cancel_boundary")

			if result, err := s.ReleaseUnpublished(t.Context(), g.Scope, g.BuildID); err != nil || result.Disposition != semantic.CandidateReleased {
				t.Fatalf("resume=%#v err=%v", result, err)
			}

			assertReleasedFamilies(t, s, g.BuildID)
		})
	}
}

func TestSemanticReleaseRevokedReopenRepair(t *testing.T) {
	s, dir := openTestStore(t, true)
	g, cov, chunks := semanticFixture()
	seedSemanticSource(t, s, g)

	if _, err := s.Stage(t.Context(), g, cov, chunks); err != nil {
		t.Fatal(err)
	}

	execTest(t, s.writer, "CREATE TRIGGER fail_release BEFORE DELETE ON semantic_chunk BEGIN SELECT RAISE(ABORT,'fail'); END")

	if _, err := s.ReleaseUnpublished(t.Context(), g.Scope, g.BuildID); err == nil {
		t.Fatal("fault did not fail")
	}

	execTest(t, s.writer, "DROP TRIGGER fail_release")

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(t.Context(), dir, Options{EmbeddingsEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	// All producers are stopped: this is the startup repair precondition.
	if err := reopened.RepairSemanticOrphans(t.Context()); err != nil {
		t.Fatal(err)
	}

	assertReleasedFamilies(t, reopened, g.BuildID)
}

func TestSemanticReleaseCapturedRowIdentityRejectsABA(t *testing.T) {
	s, g := releaseFixture(t)

	s.semanticStaging.Store(g.BuildID, semanticOperationRelease)
	defer s.semanticStaging.Delete(g.BuildID)

	var identity semanticReleaseIdentity

	if err := s.write(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
		var err error

		identity, _, err = s.revokeSemanticCandidate(ctx, tx, g.Scope, g.BuildID)

		return err
	}); err != nil {
		t.Fatal(err)
	}

	execTest(t, s.writer, "DELETE FROM semantic_build WHERE build_id=?", g.BuildID)
	peer := stageNamedSemantic(t, s, "rowid-reused-peer")

	var rowid int64
	if err := s.reader.QueryRowContext(t.Context(), "SELECT rowid FROM semantic_build WHERE build_id=?", peer.BuildID).Scan(&rowid); err != nil || rowid != identity.rowid {
		t.Fatalf("fixture did not reuse rowid: %d %d %v", rowid, identity.rowid, err)
	}

	if err := s.deleteSemanticCandidate(t.Context(), identity); !errors.Is(err, semantic.ErrConflict) {
		t.Fatalf("ABA not rejected=%v", err)
	}

	var count int
	if err := s.reader.QueryRowContext(t.Context(), "SELECT count(*) FROM semantic_chunk WHERE build_id=?", peer.BuildID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("peer rows=%d err=%v", count, err)
	}
}
