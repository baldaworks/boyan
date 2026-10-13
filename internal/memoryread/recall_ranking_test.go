package memoryread_test

import (
	"context"
	"testing"
	"time"

	read "github.com/baldaworks/boyan/internal/memoryread"
	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestRecallRanksAdmittedCandidatesBeforeTruncation(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Add(-time.Minute)
	old := recallTestRecord(t, "old", now.Add(-30*24*time.Hour))

	fresh := recallTestRecord(t, "fresh", now)
	for _, projected := range []bool{false, true} {
		service := rankingService(t, []m.RecallRecord{old, fresh}, projected)

		response, err := service.Search(context.Background(), m.RecallRequest{Scope: old.Scope, Query: "memory", Limit: 1})
		if err != nil || len(response.Results) != 1 || response.Results[0].ItemID != "fresh" {
			t.Fatalf("best admitted candidate omitted (projected=%v): %+v %v", projected, response.Results, err)
		}
	}
}

func rankingService(t *testing.T, records []m.RecallRecord, projected bool) *read.RecallService {
	t.Helper()

	var projection m.RecallProjection

	if projected {
		hits := make([]m.RecallProjectionHit, 0, len(records))
		for _, record := range records {
			hits = append(hits,
				m.RecallProjectionHit{Scope: record.Scope,
					ItemID:     record.ItemID,
					RevisionID: record.RevisionID,
					Revision:   record.Revision,
					Score:      1})
		}

		projection = recallTestProjection{hits: hits}
	}

	service, err := read.NewRecallService(&recallTestReader{records: records, seq: 2}, projection)
	if err != nil {
		t.Fatalf("service: %v", err)
	}

	return service
}

func TestRecallRankingFiltersDuplicatesAndTies(t *testing.T) {
	t.Parallel()

	observed := time.Now().UTC().Add(-time.Minute)
	first := recallTestRecord(t, "zulu", observed)
	second := recallTestRecord(t, "alpha", observed)
	invalid := recallTestRecord(t, "invalid", observed)
	invalid.State = m.RevisionStateInvalidated
	filtered := recallTestRecord(t, "filtered", observed)

	filtered.Kind = m.MemoryKindEvent
	for _, projected := range []bool{false, true} {
		service := rankingService(t, []m.RecallRecord{invalid, filtered, first, first, second}, projected)

		state := m.MemoryKindState
		for _, limit := range []int{1, 10} {
			response,
				err := service.Search(context.Background(),
				m.RecallRequest{Scope: first.Scope,
					Query: "memory",
					Kind:  &state,
					Limit: limit})
			if err != nil {
				t.Fatalf("search: %v", err)
			}

			want := min(limit, 2)
			if len(response.Results) != want || response.Results[0].ItemID != "alpha" {
				t.Fatalf("filtered/tied ranking (projected=%v): %+v",
					projected,
					response.Results)
			}
		}
	}
}
