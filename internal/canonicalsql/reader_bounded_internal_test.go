//go:build integration

package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"strconv"
	"strings"
	"testing"
)

type readerWorkQuery struct {
	base             query
	reads, envelopes int
	pageRows         map[Family]int
}

func (q *readerWorkQuery) exec(ctx context.Context, s string, args ...any) (int64, error) {
	return q.base.exec(ctx, s, args...)
}
func (q *readerWorkQuery) query(ctx context.Context, s string, args ...any) (rows, error) {
	q.reads++

	r, err := q.base.query(ctx, s, args...)
	if err != nil {
		return nil, err
	}

	envelope := strings.Contains(s, "envelope")

	var family Family

	if strings.Contains(s, "ORDER BY") && envelope {
		if strings.Contains(s, "FROM head ") || strings.Contains(s, "FROM boyan_canonical.head ") {
			family = Head
		}

		if strings.Contains(s, "source_revision") {
			family = SourceRevision
		}
	}

	return &readerWorkRows{rows: r, owner: q, envelope: envelope, family: family}, nil
}

type readerWorkRows struct {
	rows

	owner    *readerWorkQuery
	envelope bool
	family   Family
}

// Observe transferred native values, not command tuple counts: point metadata
// and absence markers carry a NULL envelope. SQLite has no metadata tuples.
func observedEnvelopeRow(r rows) (envelope, point bool) {
	native, ok := r.(pgRows)
	if !ok {
		return true, false
	}

	values := native.RawValues()
	for i, field := range native.FieldDescriptions() {
		if field.Name == "probe" {
			point = true
		}

		if (field.Name == "envelope" || field.Name == "case") && values[i] != nil {
			envelope = true
		}
	}

	return envelope, point
}

func (r *readerWorkRows) Next() bool {
	next := r.rows.Next()
	if next && r.envelope {
		envelope, point := observedEnvelopeRow(r.rows)
		if envelope {
			r.owner.envelopes++
		}

		if envelope && !point && r.family != "" {
			r.owner.pageRows[r.family]++
		}
	}

	return next
}

func TestReaderHydrationIsSourceLocalAndBounded(t *testing.T) {
	counts := make(map[string][2]int)

	for _, history := range []int{0, 512} {
		t.Run(strconv.Itoa(history), func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
				runReaderBoundedCase(t, owner, history, counts)
			})
		})
	}
}

func seedReaderHistory(t *testing.T, owner *Owner, h privacyHistory, count int) {
	t.Helper()

	if err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		for i := range count {
			id := "zz-unrelated:" + strconv.Itoa(i)

			head, _ := privacyKey(h.scope, Head, id)
			if err := putPrivacyRecord(ctx, tx, head, Head, memory.ItemHead{ItemID: id, RevisionID: id}); err != nil {
				return err
			}
			// Deliberately malformed unrelated records must never be hydrated.
			message := h.messages[0]
			message.MessageID = id
			message.SourceID = "foreign-source"

			key, _ := privacyKey(h.scope, Message, id)
			if err := putPrivacyRecord(ctx, tx, key, Message, message); err != nil {
				return err
			}

			edge := Key{Family: SourceRevision, Scope: h.scope, Parts: [][]byte{[]byte("foreign-source"), []byte(id)}}
			if err := putPrivacyRecord(ctx, tx, edge, SourceRevision, true); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func runReaderBoundedCase(t *testing.T, owner *Owner, history int, counts map[string][2]int) {
	t.Helper()

	if err := owner.PrepareSourcePrivacy(t.Context()); err != nil {
		t.Fatal(err)
	}

	h := newPrivacyHistory(t)
	if _, err := owner.ApplyCanonicalMutation(t.Context(), historyMutation(h)); err != nil {
		t.Fatal(err)
	}

	seedReaderHistory(t, owner, h, history)

	recorder := &readerWorkQuery{pageRows: make(map[Family]int)}

	var captured readerSnapshot

	if err := owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
		recorder.base = tx.q
		tx.q = recorder

		return captureReaderWork(t, ctx, tx, h, &captured)
	}); err != nil {
		t.Fatal(err)
	}

	backend := "sqlite"
	if owner.connection.pg != nil {
		backend = "postgres"
	}

	got := [2]int{recorder.reads, recorder.envelopes}
	t.Logf("backend=%s unrelated=%d reads=%d envelopes=%d page_rows=%v", backend, history, got[0], got[1],
		recorder.pageRows)

	if recorder.pageRows[Head] != 1 || recorder.pageRows[SourceRevision] != 1 {
		t.Fatalf("page hydration=%v", recorder.pageRows)
	}

	if history == 0 {
		counts[backend] = got
	} else if got != counts[backend] {
		t.Fatalf("unrelated hydration grew: %v -> %v", counts[backend], got)
	}

	if err := owner.finalReaderBarrier(t.Context(), captured); err != nil {
		t.Fatal(err)
	}
}

func captureReaderWork(t *testing.T, ctx context.Context, tx *Tx, h privacyHistory, captured *readerSnapshot) error {
	t.Helper()

	records, err := readerRecallTail(ctx, tx,
		memory.RecallRequest{Scope: h.scope, Query: "protected", Limit: 1}, 1, captured)
	if err != nil {
		return err
	}

	if len(records) != 1 || records[0].Text != "protected memory" {
		t.Fatalf("records=%+v", records)
	}

	ids, err := scanReaderSource(ctx, tx, h.scope, h.source.SourceID, "", 1)
	if err == nil && (len(ids) != 1 || ids[0] != h.revision.RevisionID) {
		t.Fatalf("source page=%v", ids)
	}

	return err
}
