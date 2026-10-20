//go:build integration

//nolint:lll // Real SQL mutations target the unlocked candidate/page boundary.
package sqlite

import (
	"context"
	"database/sql"
	"testing"

	semantic "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

func TestSemanticChunkScrubRechecksOriginalSource(t *testing.T) {
	s, _ := openTestStore(t, true)
	g, r, c := semanticFixture()
	seedSemanticSource(t, s, g)

	c[0].SourceIDs = []string{"source"}
	registerSemanticPrivacy(t, s, g, r, c)

	if _, err := s.Stage(t.Context(), g, r, c); err != nil {
		t.Fatal(err)
	}

	if err := s.Publish(t.Context(), semantic.Publication{View: g.View(), BuildID: g.BuildID}); err != nil {
		t.Fatal(err)
	}

	predicate, args := scrubPredicate(g.Scope, []string{"source"}, true, true)
	query := `SELECT r.rowid AS id,1,r.build_id AS owner,r.revision_id AS revision FROM semantic_chunk r JOIN semantic_build b USING(build_id) WHERE ` + predicate + ` ORDER BY r.rowid LIMIT 128`

	rows, err := s.scrubCandidates(t.Context(), query, args)
	if err != nil || len(rows) != 1 {
		t.Fatalf("candidates=%#v %v", rows, err)
	}

	execTest(t, s.writer, "UPDATE semantic_source SET id='other-source'")

	a, err := NewSQLiteCanonicalApplier(s, &applyReader{}, fixtureCanonicalPrivacy(t, s, &applyReader{}))
	if err != nil {
		t.Fatal(err)
	}

	if err := a.deleteScrubRows(t.Context(), rows, true); err != nil {
		t.Fatal(err)
	}

	state, found, err := s.ActiveState(t.Context(), g.Scope)
	if err != nil || !found || state.Status != semantic.StatusReady {
		t.Fatalf("source no longer matches=%#v %v %v", state, found, err)
	}
}

func TestSemanticCoverageScrubRechecksSourceAndReusedOwnership(t *testing.T) {
	for _, boundary := range []string{"source changed", "row reused other kind"} {
		t.Run(boundary, func(t *testing.T) {
			s, _ := openTestStore(t, true)
			g, _, _ := semanticFixture()
			seedLexical(t, s, "proof", g.Scope, []lexicalFixture{{id: "revision", raw: "needle", sources: []string{"source"}}})
			seedZeroCoverage(t, s, "pending", g.Scope, "revision")
			execTest(t, s.writer, "UPDATE semantic_build SET complete=0 WHERE build_id='pending'")

			query := `SELECT c.rowid AS id,1,c.build_id AS owner,c.revision_id AS revision
 FROM semantic_coverage c JOIN semantic_build b USING(build_id)
 WHERE b.scope_kind=? AND b.scope_key=? AND c.revision_id='revision'
 AND NOT EXISTS(SELECT 1 FROM semantic_chunk r WHERE r.build_id=c.build_id AND r.revision_id=c.revision_id)
 AND EXISTS(SELECT 1 FROM lexical_record l JOIN lexical_build lb USING(physical_build_id)
 JOIN lexical_source p ON p.record_rowid=l.rowid WHERE p.id='source' AND l.revision_id=c.revision_id
 AND lb.scope_kind=b.scope_kind AND lb.scope_key=b.scope_key) ORDER BY c.rowid LIMIT 128`

			rows, err := s.scrubCandidates(t.Context(), query, []any{string(g.Scope.Kind), g.Scope.Key})
			if err != nil || len(rows) != 1 {
				t.Fatalf("candidates=%#v %v", rows, err)
			}

			if boundary == "source changed" {
				execTest(t, s.writer, "UPDATE lexical_source SET id='other-source'")
			} else {
				execTest(t, s.writer, "DELETE FROM semantic_build WHERE build_id='pending'")

				other := g.Scope
				other.Kind = memory.ScopeKindGroup
				seedZeroCoverage(t, s, "other-kind", other, "revision")
			}

			if err := s.write(t.Context(), func(ctx context.Context, tx *sql.Tx) error { return scrubZeroCoverageRows(ctx, tx, rows) }); err != nil {
				t.Fatal(err)
			}

			var n int
			if err := s.reader.QueryRowContext(t.Context(), "SELECT count(*) FROM semantic_coverage").Scan(&n); err != nil || n != 1 {
				t.Fatalf("unrelated coverage removed=%d %v", n, err)
			}
		})
	}
}
