package memoryread_test

import (
	"context"
	"testing"
	"time"

	read "github.com/baldaworks/boyan/internal/memoryread"
	m "github.com/baldaworks/boyan/pkg/memory"
)

type recallTestReader struct {
	records   []m.RecallRecord
	seq       uint64
	tailCalls int
}

func (r *recallTestReader) CurrentScopeChangeSeq(context.Context, m.Scope) (uint64, error) {
	return r.seq, nil
}
func (r *recallTestReader) LoadRecallRecords(_ context.Context, _ m.Scope, ids []string) ([]m.RecallRecord, error) {
	result := make([]m.RecallRecord, 0, len(ids))
	for _, record := range r.records {
		for _, id := range ids {
			if record.RevisionID == id {
				result = append(result, record)
			}
		}
	}

	return result, nil
}
func (r *recallTestReader) SearchRecallTail(context.Context, m.RecallRequest, uint32) ([]m.RecallRecord, error) {
	r.tailCalls++

	return r.records, nil
}

type recallTestProjection struct{ hits []m.RecallProjectionHit }

func (p recallTestProjection) SearchRecall(context.Context, m.RecallRequest) ([]m.RecallProjectionHit, error) {
	return p.hits, nil
}
func recallTestRecord(t *testing.T, id string, observed time.Time) m.RecallRecord {
	t.Helper()

	evidence, err := m.NewEvidenceRef("source", "message", m.MessageRoleUser, "memory", 0, 6, m.AssertionModeUser)
	if err != nil {
		t.Fatalf("evidence: %v", err)
	}

	return m.RecallRecord{Scope: m.Scope{Key: "test:recall",
		Kind: m.ScopeKindPersonal},
		ItemID:         id,
		RevisionID:     id + "-revision",
		Revision:       1,
		Kind:           m.MemoryKindState,
		Text:           "memory",
		State:          m.RevisionStateActive,
		CreatedAt:      observed,
		Temporal:       m.Temporal{ObservedAt: observed},
		Sensitivity:    m.SensitivityStandard,
		Retention:      m.RetentionClassStandard,
		Evidence:       []m.EvidenceRef{evidence},
		SourceIDs:      []string{"source"},
		ScopeChangeSeq: 2}
}

func TestRecallMinimumRejectsUncertifiedProjection(t *testing.T) {
	t.Parallel()
	old := recallTestRecord(t, "old", time.Now().UTC().Add(-time.Hour))

	fresh := recallTestRecord(t, "fresh", time.Now().UTC())
	for _, projection := range []m.RecallProjection{nil,
		recallTestProjection{hits: []m.RecallProjectionHit{{Scope: old.Scope,
			ItemID:         old.ItemID,
			RevisionID:     old.RevisionID,
			Revision:       1,
			Score:          1,
			ScopeChangeSeq: 1}}}} {
		reader := &recallTestReader{records: []m.RecallRecord{old, fresh}, seq: 2}

		service, err := read.NewRecallService(reader, projection)
		if err != nil {
			t.Fatalf("service: %v", err)
		}

		_,
			err = service.Search(context.Background(),
			m.RecallRequest{Scope: old.Scope,
				Query:             "memory",
				Limit:             10,
				MinScopeChangeSeq: 2})

		code, class, ok := m.ClassifyError(err)
		if !ok || code != m.CodeConflict || class != m.ErrorClassRetryable {
			t.Fatalf("uncertified minimum succeeded or wrong error: %v", err)
		}
	}
}

type certifiedTestProjection struct {
	recallTestProjection

	err error
}

func (p certifiedTestProjection) SearchRecallConsistent(context.Context,
	m.RecallRequest) ([]m.RecallProjectionHit,
	error) {
	return p.hits,
		p.err
}

func TestRecallCertifiedEmptyDoesNotUseTail(t *testing.T) {
	t.Parallel()
	record := recallTestRecord(t, "memory", time.Now().UTC())
	reader := &recallTestReader{records: []m.RecallRecord{record}, seq: 2}

	service, err := read.NewRecallService(reader, certifiedTestProjection{})
	if err != nil {
		t.Fatalf("service: %v", err)
	}

	response,
		err := service.Search(context.Background(),
		m.RecallRequest{Scope: record.Scope,
			Query:             "memory",
			Limit:             10,
			MinScopeChangeSeq: 2})
	if err != nil || len(response.Results) != 0 || reader.tailCalls != 0 {
		t.Fatalf("uncertified tail used: %+v %v calls=%d",
			response,
			err,
			reader.tailCalls)
	}

	_,
		err = service.Search(context.Background(),
		m.RecallRequest{Scope: record.Scope,
			Query:             "memory",
			Limit:             10,
			MinScopeChangeSeq: 3})

	code, _, ok := m.ClassifyError(err)
	if !ok || code != m.CodeConflict {
		t.Fatalf("canonical sequence below minimum accepted: %v", err)
	}
}
