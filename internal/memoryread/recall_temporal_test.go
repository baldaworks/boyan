package memoryread_test

import (
	"context"
	"testing"
	"time"

	read "github.com/baldaworks/boyan/internal/memoryread"
	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestRecallEffectiveTimeWindows(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 13, 12, 0, 0, 0, time.UTC)
	before := now.Add(-time.Hour)
	after := now.Add(time.Hour)

	cases := []struct {
		name   string
		change func(*m.RecallRecord)
		valid  bool
	}{
		{"unbounded", func(*m.RecallRecord) {}, true},
		{"future observation", func(r *m.RecallRecord) { r.Temporal.ObservedAt = after; r.CreatedAt = after }, false},
		{"observation equality", func(r *m.RecallRecord) { r.Temporal.ObservedAt = now }, true},
		{"future validity", func(r *m.RecallRecord) { r.Temporal.ValidFrom = &after }, false},
		{"validity start equality", func(r *m.RecallRecord) { r.Temporal.ValidFrom = &now }, true},
		{"past validity end", func(r *m.RecallRecord) { r.Temporal.ValidUntil = &before }, false},
		{"validity end equality", func(r *m.RecallRecord) { r.Temporal.ValidUntil = &now }, false},
		{"past expiry", func(r *m.RecallRecord) { r.Temporal.ExpiresAt = &before }, false},
		{"expiry equality", func(r *m.RecallRecord) { r.Temporal.ExpiresAt = &now }, false},
		{"future end", func(r *m.RecallRecord) { r.Temporal.ValidUntil = &after; r.Temporal.ExpiresAt = &after }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			record := recallTestRecord(t, "window", before)
			tc.change(&record)

			for _, asOf := range []*time.Time{nil, &now} {
				request := m.RecallRequest{Scope: record.Scope, Query: "memory", Limit: 10, AsOf: asOf}

				err := record.Validate(request, now)
				if tc.valid && err != nil {
					t.Fatalf("valid window rejected: %v", err)
				}

				if !tc.valid {
					code, _, ok := m.ClassifyError(err)
					if !ok || code != m.CodeNotFound {
						t.Fatalf("excluded window accepted/wrong error: %v", err)
					}
				}
			}
		})
	}
}

func TestRecallTimezonesAndReadPaths(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	future := now.Add(24 * time.Hour)
	record := recallTestRecord(t, "future", future)
	offset := now.In(time.FixedZone("Bishkek", 6*60*60))

	hit := m.RecallProjectionHit{Scope: record.Scope,
		ItemID:     record.ItemID,
		RevisionID: record.RevisionID,
		Revision:   1,
		Score:      1}
	for _, projection := range []m.RecallProjection{nil, recallTestProjection{hits: []m.RecallProjectionHit{hit}}} {
		service, err := read.NewRecallService(&recallTestReader{records: []m.RecallRecord{record}, seq: 2}, projection)
		if err != nil {
			t.Fatalf("service: %v", err)
		}

		for _, asOf := range []*time.Time{nil, &now, &offset} {
			response,
				searchErr := service.Search(context.Background(),
				m.RecallRequest{Scope: record.Scope,
					Query: "memory",
					Limit: 10,
					AsOf:  asOf})
			if searchErr != nil || len(response.Results) != 0 {
				t.Fatalf("future record exposed: %+v %v", response, searchErr)
			}
		}
	}
}
