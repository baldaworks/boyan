//nolint:cyclop,funlen,lll // Channel and actual SQL barriers establish operation ordering without sleep assumptions.
package sqlite

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	semantic "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

func releaseBarrier(t *testing.T) (string, <-chan struct{}, func()) {
	t.Helper()

	entered, resume := make(chan struct{}), make(chan struct{})

	var enterOnce, resumeOnce sync.Once

	name := releaseSQLFunction(t, func() { enterOnce.Do(func() { close(entered); <-resume }) })

	return name, entered, func() { resumeOnce.Do(func() { close(resume) }) }
}

func awaitReleaseReservation(t *testing.T, s *Store, build string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	for {
		if value, ok := s.semanticStaging.Load(build); ok && value == semanticOperationRelease {
			return
		}

		if ctx.Err() != nil {
			t.Fatalf("release did not reserve: %v", ctx.Err())
		}

		runtime.Gosched()
	}
}

func TestSemanticReleaseLiveStageRejectsWithoutWaiting(t *testing.T) {
	name, entered, resume := releaseBarrier(t)
	s, _ := openTestStore(t, true)

	defer resume()

	g, cov, chunks := semanticFixture()
	seedSemanticSource(t, s, g)
	execTest(t, s.writer, "CREATE TRIGGER stage_pause AFTER INSERT ON semantic_chunk BEGIN SELECT "+name+"(); END")

	staged := make(chan error, 1)

	go func() { _, err := s.Stage(t.Context(), g, cov, chunks); staged <- err }()

	<-entered

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	if result, err := s.ReleaseUnpublished(ctx, g.Scope, g.BuildID); !errors.Is(err, semantic.ErrConflict) || result != (semantic.CandidateRelease{}) {
		t.Fatalf("live stage result=%#v err=%v", result, err)
	}

	resume()

	if err := <-staged; err != nil {
		t.Fatal(err)
	}

	if result, err := s.ReleaseUnpublished(t.Context(), g.Scope, g.BuildID); err != nil || result.Disposition != semantic.CandidateReleased {
		t.Fatalf("ended stage result=%#v err=%v", result, err)
	}

	assertReleasedFamilies(t, s, g.BuildID)
}

func TestSemanticReleaseReservationExcludesStageReleaseAndGC(t *testing.T) {
	s, g := releaseFixture(t)
	peer := stageNamedSemantic(t, s, "complete-peer")
	other, cov, chunks := semanticFixture()
	other.Scope.Kind = memory.ScopeKindGroup
	other.BuildID = "other-source"
	seedSemanticSource(t, s, other)
	other.BuildID = g.BuildID
	chunks[0].Scope = other.Scope

	execTest(t, s.writer, "UPDATE semantic_build SET complete=0 WHERE build_id=?", g.BuildID)

	unlock, err := s.lockScope(t.Context(), g.Scope)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if unlock != nil {
			unlock()
		}
	}()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	released := make(chan error, 1)

	go func() { _, err := s.ReleaseUnpublished(ctx, g.Scope, g.BuildID); released <- err }()

	awaitReleaseReservation(t, s, g.BuildID)

	if _, err := s.ReleaseUnpublished(t.Context(), g.Scope, g.BuildID); !errors.Is(err, semantic.ErrConflict) {
		t.Fatalf("simultaneous release=%v", err)
	}

	if _, err := s.Stage(t.Context(), other, cov, chunks); !errors.Is(err, semantic.ErrConflict) {
		t.Fatalf("Stage crossed reservation=%v", err)
	}

	if err := s.CollectGarbage(t.Context()); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := s.reader.QueryRowContext(t.Context(), "SELECT count(*) FROM semantic_build WHERE build_id IN (?,?)", g.BuildID, peer.BuildID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("GC crossed reservation/peer=%d err=%v", count, err)
	}

	cancel()

	if err := <-released; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting release=%v", err)
	}

	if _, ok := s.semanticStaging.Load(g.BuildID); ok {
		t.Fatal("canceled reservation leaked")
	}

	unlock()
	unlock = nil

	if result, err := s.ReleaseUnpublished(t.Context(), g.Scope, g.BuildID); err != nil || result.Disposition != semantic.CandidateReleased {
		t.Fatalf("resume=%#v err=%v", result, err)
	}

	assertReleasedFamilies(t, s, g.BuildID)
}

func TestSemanticReleasePublishWinsBeforeRevocation(t *testing.T) {
	name, entered, resume := releaseBarrier(t)
	s, g := releaseFixture(t)

	defer resume()

	execTest(t, s.writer, "CREATE TRIGGER publish_pause BEFORE INSERT ON semantic_active BEGIN SELECT "+name+"(); END")

	published := make(chan error, 1)
	go func() { published <- s.Publish(t.Context(), semantic.Publication{View: g.View(), BuildID: g.BuildID}) }()

	<-entered

	type outcome struct {
		result semantic.CandidateRelease
		err    error
	}

	released := make(chan outcome, 1)

	go func() {
		result, err := s.ReleaseUnpublished(t.Context(), g.Scope, g.BuildID)
		released <- outcome{result, err}
	}()

	awaitReleaseReservation(t, s, g.BuildID)
	resume()

	if err := <-published; err != nil {
		t.Fatal(err)
	}

	got := <-released
	if got.err != nil || got.result.Disposition != semantic.CandidatePublished || got.result.Epoch != 1 {
		t.Fatalf("publish winner=%#v err=%v", got.result, got.err)
	}

	state, found, err := s.ActiveState(t.Context(), g.Scope)
	if err != nil || !found || state.BuildID != g.BuildID || state.ChunkCount != 1 {
		t.Fatalf("active=%#v found=%v err=%v", state, found, err)
	}
}

func TestSemanticReleaseRevocationWinsAndPreservesActiveReader(t *testing.T) {
	name, entered, resume := releaseBarrier(t)
	s, g := releaseFixture(t)

	defer resume()

	prior := stageNamedSemantic(t, s, "prior-active")
	if err := s.Publish(t.Context(), semantic.Publication{View: prior.View(), BuildID: prior.BuildID}); err != nil {
		t.Fatal(err)
	}

	snapshot, err := s.read(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = snapshot.Close() }()

	if _, found, err := activeSemanticState(snapshot.ctx, snapshot.tx, g.Scope); err != nil || !found {
		t.Fatalf("pin active=%v %v", found, err)
	}

	execTest(t, s.writer, "CREATE TRIGGER revoke_pause AFTER UPDATE OF complete ON semantic_build WHEN NEW.build_id='semantic-build' AND NEW.complete=0 BEGIN SELECT "+name+"(); END")

	released := make(chan error, 1)

	go func() {
		result, err := s.ReleaseUnpublished(t.Context(), g.Scope, g.BuildID)
		if err == nil && result.Disposition != semantic.CandidateReleased {
			err = errors.New("release did not complete")
		}

		released <- err
	}()

	<-entered

	published := make(chan error, 1)
	go func() {
		published <- s.Publish(t.Context(), semantic.Publication{View: g.View(), BuildID: g.BuildID, ExpectedActiveBuildID: prior.BuildID, ExpectedActiveEpoch: 1})
	}()

	resume()

	if err := <-released; err != nil {
		t.Fatal(err)
	}

	if err := <-published; !errors.Is(err, semantic.ErrConflict) && !errors.Is(err, semantic.ErrCorrupt) {
		t.Fatalf("revoked candidate published=%v", err)
	}

	assertReleasedFamilies(t, s, g.BuildID)

	for _, read := range []bool{false, true} {
		var (
			state semantic.State
			found bool
			err   error
		)
		if read {
			state, found, err = activeSemanticState(snapshot.ctx, snapshot.tx, g.Scope)
		} else {
			state, found, err = s.ActiveState(t.Context(), g.Scope)
		}

		if err != nil || !found || state.BuildID != prior.BuildID || state.PublicationEpoch != 1 {
			t.Fatalf("active reader=%t state=%#v found=%v err=%v", read, state, found, err)
		}
	}
}

func TestSemanticReleaseCloseCancelsReservationAndDrains(t *testing.T) {
	s, g := releaseFixture(t)

	unlock, err := s.lockScope(t.Context(), g.Scope)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	released := make(chan error, 1)

	go func() { _, err := s.ReleaseUnpublished(t.Context(), g.Scope, g.BuildID); released <- err }()

	awaitReleaseReservation(t, s, g.BuildID)

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	if err := <-released; !errors.Is(err, context.Canceled) {
		t.Fatalf("lifetime cancellation=%v", err)
	}

	if _, live := s.semanticStaging.Load(g.BuildID); live {
		t.Fatal("Close returned before reservation drained")
	}

	if _, err := s.ReleaseUnpublished(t.Context(), g.Scope, g.BuildID); !errors.Is(err, semantic.ErrClosed) {
		t.Fatalf("closed=%v", err)
	}
}

func TestSemanticReleaseCloseCancelsInFlightSQL(t *testing.T) {
	name, entered, resume := releaseBarrier(t)
	s, g := releaseFixture(t)

	defer resume()

	execTest(t, s.writer, "CREATE TRIGGER release_pause BEFORE DELETE ON semantic_chunk BEGIN SELECT "+name+"(); END")

	released := make(chan error, 1)

	go func() { _, err := s.ReleaseUnpublished(t.Context(), g.Scope, g.BuildID); released <- err }()

	<-entered

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := s.CloseContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("blocked SQL CloseContext=%v", err)
	}

	resume()

	if err := <-released; !errors.Is(err, context.Canceled) {
		t.Fatalf("SQL lifetime cancellation=%v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	if _, live := s.semanticStaging.Load(g.BuildID); live {
		t.Fatal("Close returned before SQL reservation drained")
	}
}
