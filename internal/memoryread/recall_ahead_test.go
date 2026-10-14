package memoryread_test

import (
	"context"
	"errors"
	"testing"
	"time"

	read "github.com/baldaworks/boyan/internal/memoryread"
	m "github.com/baldaworks/boyan/pkg/memory"
)

type advancingRecallReader struct {
	recallTestReader

	after uint64
	err   error
	reads int
}

func (r *advancingRecallReader) CurrentScopeChangeSeq(ctx context.Context, scope m.Scope) (uint64, error) {
	r.reads++
	if r.reads == 1 {
		return r.recallTestReader.CurrentScopeChangeSeq(ctx, scope)
	}

	if err := ctx.Err(); err != nil {
		return 0, err
	}

	return r.after, r.err
}

func TestRecallAheadProjectionRequiresCanonicalCoverage(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		after    uint64
		otherHit uint64
		class    m.ErrorClass
	}{
		{"unchanged canonical", 1, 2, m.ErrorClassPermanent},
		{"insufficient progress", 2, 3, m.ErrorClassPermanent},
		{"all hits covered", 3, 3, m.ErrorClassRetryable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			first := recallTestRecord(t, "first", time.Now().UTC().Add(-time.Hour))
			second := recallTestRecord(t, "second", first.CreatedAt)
			reader := &advancingRecallReader{
				recallTestReader: recallTestReader{records: []m.RecallRecord{first, second}, seq: 1},
				after:            tc.after,
			}
			hits := []m.RecallProjectionHit{
				{Scope: first.Scope, RevisionID: first.RevisionID, ScopeChangeSeq: 2},
				{Scope: second.Scope, RevisionID: second.RevisionID, ScopeChangeSeq: tc.otherHit},
			}

			service, err := read.NewRecallService(reader, recallTestProjection{hits: hits})
			if err != nil {
				t.Fatal(err)
			}

			response, err := service.Search(context.Background(),
				m.RecallRequest{Scope: first.Scope, Query: "memory", Limit: 10})

			code, class, ok := m.ClassifyError(err)
			if !ok || code != m.CodeConflict || class != tc.class {
				t.Fatalf("expected conflict class %s, got %v", tc.class, err)
			}

			if response.ScopeChangeSeq != 0 || len(response.Results) != 0 {
				t.Fatalf("partial response: %+v", response)
			}
		})
	}
}

func TestRecallAheadProjectionPropagatesCanonicalReadFailure(t *testing.T) {
	t.Parallel()

	failure := errors.New("canonical read failed")

	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "read error", true: "canceled context"}[canceled], func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			record := recallTestRecord(t, "memory", time.Now().UTC().Add(-time.Hour))
			reader := &advancingRecallReader{
				recallTestReader: recallTestReader{records: []m.RecallRecord{record}, seq: 1},
				after:            2, err: failure,
			}
			projection := &cancelingRecallProjection{recallTestProjection: recallTestProjection{
				hits: []m.RecallProjectionHit{{Scope: record.Scope, RevisionID: record.RevisionID, ScopeChangeSeq: 2}},
			}}
			want := failure

			if canceled {
				projection.before = cancel
				want = context.Canceled
			}

			service, err := read.NewRecallService(reader, projection)
			if err != nil {
				t.Fatal(err)
			}

			response, err := service.Search(ctx, m.RecallRequest{Scope: record.Scope, Query: "memory", Limit: 10})
			if !errors.Is(err, want) {
				t.Fatalf("expected read failure %v, got %v", want, err)
			}

			if response.ScopeChangeSeq != 0 || len(response.Results) != 0 {
				t.Fatalf("partial response: %+v", response)
			}
		})
	}
}

type cancelingRecallProjection struct {
	recallTestProjection

	before func()
}

func (p *cancelingRecallProjection) SearchRecall(
	ctx context.Context, request m.RecallRequest,
) ([]m.RecallProjectionHit, error) {
	if p.before != nil {
		p.before()
	}

	return p.recallTestProjection.SearchRecall(ctx, request)
}
