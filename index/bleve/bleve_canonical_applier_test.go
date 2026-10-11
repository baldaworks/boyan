package bleve_test

import bleve "github.com/baldaworks/boyan/index/bleve"

import (
	"context"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

type canonicalRecordReader struct {
	memory.RecallCanonicalReader

	records map[string]memory.RecallRecord
}

func (r *canonicalRecordReader) LoadRecallRecords(
	_ context.Context,
	scope memory.Scope,
	ids []string,
) (
	[]memory.RecallRecord,
	error,
) {
	records := make([]memory.RecallRecord, 0, len(ids))
	for _, id := range ids {
		if record, ok := r.records[id]; ok {
			record.Scope = scope
			records = append(records, record)
		}
	}

	return records, nil
}

func TestCanonicalApplierResumesAndDeletes(t *testing.T) {
	projection, err := bleve.Open(t.TempDir())
	requireNoError(t, err, "%v")

	reader := canonicalApplierTestReader()

	applier, err := bleve.NewBleveCanonicalApplier(projection, reader)
	requireNoError(t, err, "%v")

	defer applier.Close()

	scope := memory.Scope{Kind: memory.ScopeKindPersonal, Key: "test:scope"}
	requireNoError(t, applier.BeginProjectionRebuild(context.Background(), scope, "first"), "%v")

	apply := func(scope memory.Scope, generation, id string) {
		t.Helper()

		requireNoError(t, applier.Apply(
			context.Background(),
			scope,
			generation,
			nil,
			[]memory.MemoryRevision{{RevisionID: id}},
		), "%v")

		requireNoError(t, applier.Commit(context.Background(), scope, generation), "%v")
	}
	apply(scope, "first", "one")
	apply(scope, "first", "two")

	requireNoError(t, applier.Activate(context.Background(), scope, "first"), "%v")

	if resumed, err := applier.ResumeProjection(context.Background(), scope, "first"); err != nil || !resumed {
		t.Fatalf("resume: %v %v", resumed, err)
	}

	apply(scope, "first", "three")

	requireNoError(t, applier.Activate(context.Background(), scope, "first"), "%v")

	assertCanonicalApplierHits(t, projection, scope, 3)

	other := memory.Scope{Kind: memory.ScopeKindGroup, Key: scope.Key}
	requireNoError(t, applier.BeginProjectionRebuild(context.Background(), other, "second"), "%v")

	apply(other, "second", "one")

	requireNoError(t, applier.Activate(context.Background(), other, "second"), "%v")

	delete(reader.records, "one")
	apply(scope, "first", "one")

	requireNoError(t, applier.Activate(context.Background(), scope, "first"), "%v")

	assertCanonicalApplierHits(t, projection, scope, 2)
	assertCanonicalApplierHits(t, projection, other, 1)
}

func canonicalApplierTestReader() *canonicalRecordReader {
	reader := &canonicalRecordReader{records: map[string]memory.RecallRecord{}}

	for _, id := range []string{"one", "two", "three"} {
		now := time.Now().UTC()
		reader.records[id] = memory.RecallRecord{
			ItemID:          id,
			RevisionID:      id,
			Revision:        1,
			Kind:            memory.MemoryKindEvent,
			Text:            "shared searchable memory",
			CreatedAt:       now,
			Temporal:        memory.Temporal{ObservedAt: now},
			Sensitivity:     memory.SensitivityStandard,
			Retention:       memory.RetentionClassStandard,
			SourceIDs:       []string{"source"},
			ConversationIDs: []string{"conversation"},
			ScopeChangeSeq:  1,
		}
	}

	return reader
}

func assertCanonicalApplierHits(t *testing.T, projection *bleve.BleveRecallProjection, scope memory.Scope, want int) {
	t.Helper()

	hits, err := projection.SearchRecall(
		context.Background(),
		memory.RecallRequest{Scope: scope, Query: "searchable", Limit: 10},
	)
	if err != nil || len(hits) != want {
		t.Fatalf("hits: %v %v, want %d", hits, err, want)
	}
}
