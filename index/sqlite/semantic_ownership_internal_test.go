//nolint:cyclop,funlen,lll // Real SQL staging pauses exercise scope ownership and cancellation.
package sqlite

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"
	"time"

	semantic "github.com/baldaworks/boyan/internal/semantic"
	sqlitedriver "modernc.org/sqlite"
)

func TestSemanticLiveStageExcludesForgetAndRejectsPostForgetSource(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})

	name := fmt.Sprintf("pause_semantic_stage_%d", semanticCancelSequence.Add(1))
	if err := sqlitedriver.RegisterScalarFunction(name, 0, func(*sqlitedriver.FunctionContext, []driver.Value) (driver.Value, error) {
		close(entered)
		<-release

		return int64(0), nil
	}); err != nil {
		t.Fatal(err)
	}

	s, _ := openTestStore(t, true)
	g, r, c := semanticFixture()
	c[0].SourceIDs = []string{"source"}

	seedLexical(t, s, "source", g.Scope, []lexicalFixture{{id: "revision", raw: "needle", sources: []string{"source"}}})
	execTest(t, s.writer, "UPDATE lexical_build SET generation_id=?,watermark=?,record_count=1,source_count=1", g.LexicalGenerationID, encodeUint64(1))
	execTest(t, s.writer, "CREATE TRIGGER pause_stage AFTER INSERT ON semantic_chunk BEGIN SELECT "+name+"(); END")
	registerSemanticPrivacy(t, s, g, r, c)

	staged := make(chan error, 1)

	go func() { _, err := s.Stage(t.Context(), g, r, c); staged <- err }()

	select {
	case <-entered:
	case err := <-staged:
		t.Fatalf("Stage exited before SQL pause: %v", err)
	}

	a, err := NewSQLiteCanonicalApplier(s, &applyReader{}, fixtureCanonicalPrivacy(t, s, &applyReader{}))
	if err != nil {
		t.Fatal(err)
	}

	canceled, cancel := context.WithCancel(t.Context())
	cancel()

	if err := a.ScrubCanonicalForget(canceled, g.Scope, []string{"source"}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting scrub=%v", err)
	}

	close(release)

	if err := <-staged; err != nil {
		t.Fatal(err)
	}

	execTest(t, s.writer, "DROP TRIGGER pause_stage")

	if err := a.ScrubCanonicalForget(t.Context(), g.Scope, []string{"source"}, nil); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := s.reader.QueryRowContext(t.Context(), "SELECT count(*) FROM semantic_chunk").Scan(&n); err != nil || n != 0 {
		t.Fatalf("forgotten stage rows=%d %v", n, err)
	}

	g.BuildID = "late"

	c[0].BuildID = g.BuildID
	if _, err := s.Stage(t.Context(), g, r, c); !errors.Is(err, semantic.ErrConflict) {
		t.Fatalf("postforget source accepted=%v", err)
	}
}

func TestSemanticScopeWaitCancellationAndCloseDrain(t *testing.T) {
	s, _ := openTestStore(t, true)
	g, r, c := semanticFixture()
	seedSemanticSource(t, s, g)

	unlock, err := s.lockScope(t.Context(), g.Scope)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()

	if _, err := s.Stage(ctx, g, r, c); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait cancellation=%v", err)
	}

	unlock()

	if _, err := s.Stage(t.Context(), g, r, c); err != nil {
		t.Fatalf("lease leaked=%v", err)
	}

	unlock, err = s.lockScope(t.Context(), g.Scope)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	waiting := make(chan error, 1)
	go func() { waiting <- s.Publish(t.Context(), semantic.Publication{View: g.View(), BuildID: g.BuildID}) }()

	if err := s.Close(); err != nil {
		t.Fatalf("close drain=%v", err)
	}

	if err := <-waiting; !errors.Is(err, context.Canceled) && !errors.Is(err, semantic.ErrClosed) {
		t.Fatalf("closed waiter=%v", err)
	}
}

func TestSemanticForgetPreservesProofAcrossCoveragePages(t *testing.T) {
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

	for i := range 130 {
		build := fmt.Sprintf("pending-%03d", i)
		seedZeroCoverage(t, s, build, g.Scope, "revision")
		execTest(t, s.writer, "UPDATE semantic_build SET complete=0 WHERE build_id=?", build)
	}

	a, err := NewSQLiteCanonicalApplier(s, &applyReader{}, fixtureCanonicalPrivacy(t, s, &applyReader{}))
	if err != nil {
		t.Fatal(err)
	}

	if err := a.ScrubCanonicalForget(t.Context(), g.Scope, []string{"source"}, nil); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := s.reader.QueryRowContext(t.Context(), "SELECT count(*) FROM semantic_coverage").Scan(&n); err != nil || n != 0 {
		t.Fatalf("coverage proof lost across pages=%d %v", n, err)
	}

	state, found, err := s.ActiveState(t.Context(), g.Scope)
	if err != nil || !found || state.Status != semantic.StatusDegraded {
		t.Fatalf("active retirement=%#v %v %v", state, found, err)
	}
}
