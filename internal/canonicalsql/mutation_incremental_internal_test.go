package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"strconv"
	"strings"
	"testing"
)

func TestMutationUnrelatedHistoryDoesNotAddWork(t *testing.T) {
	counts := make(map[string][3]int)

	for _, history := range []int{0, 1024} {
		t.Run(strconv.Itoa(history), func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
				runMutationHistoryWork(t, owner, history, counts)
			})
		})
	}
}

type mutationWorkQuery struct {
	base                query
	reads, rows, writes int
}

func (q *mutationWorkQuery) exec(ctx context.Context, s string, args ...any) (int64, error) {
	q.writes++

	return q.base.exec(ctx, s, args...)
}
func (q *mutationWorkQuery) query(ctx context.Context, s string, args ...any) (rows, error) {
	q.reads++

	if strings.Contains(s, "ORDER BY") {
		return nil, invalid("ordinary mutation sorted history")
	}

	r, err := q.base.query(ctx, s, args...)
	if err != nil {
		return nil, err
	}

	return &mutationWorkRows{rows: r, owner: q}, nil
}

type mutationWorkRows struct {
	rows

	owner *mutationWorkQuery
}

func (r *mutationWorkRows) Next() bool {
	next := r.rows.Next()
	if next {
		r.owner.rows++
	}

	return next
}

func historyMutation(h privacyHistory) memory.CanonicalMutation {
	return memory.CanonicalMutation{SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: h.scope,
		Operation: memory.OperationRecord{OperationID: "initial", Fingerprint: "initial",
			CommittedAt: h.turn.CompletedAt}, Sources: []memory.CanonicalSourceRecord{h.source}, Messages: h.messages,
		Items: []memory.MemoryItem{h.item}, Revisions: []memory.MemoryRevision{h.revision},
		Heads: []memory.ItemHead{{ItemID: h.item.ItemID, RevisionID: h.revision.RevisionID}}, Payloads: h.payloads}
}

func runMutationHistoryWork(t *testing.T, owner *Owner, history int, counts map[string][3]int) {
	t.Helper()

	if err := owner.PrepareSourcePrivacy(t.Context()); err != nil {
		t.Fatal(err)
	}

	h := newPrivacyHistory(t)
	seedUnrelatedMutationHistory(t, owner, h.scope, history)

	mutation := historyMutation(h)

	boundary, err := memory.NewBoundary(h.scope, h.turn.Conversation, "close", memory.BoundaryReasonClose,
		h.turn.CompletedAt)
	if err != nil {
		t.Fatal(err)
	}

	identity, err := memory.NewCanonicalBoundaryIdentityCommit(boundary)
	if err != nil {
		t.Fatal(err)
	}

	mutation.BoundaryIdentity = &identity

	var recorder *mutationWorkQuery

	if err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		recorder = &mutationWorkQuery{base: tx.q}
		tx.q = recorder
		_, err := applyCanonicalMutationTxn(ctx, tx, mutation)

		return err
	}); err != nil {
		t.Fatal(err)
	}

	backend := "sqlite"
	if owner.connection.pg != nil {
		backend = "postgres"
	}

	actual := [3]int{recorder.reads, recorder.rows, recorder.writes}
	t.Logf("backend=%s unrelated=%d statements_read=%d rows_read=%d statements_write=%d", backend, history,
		actual[0], actual[1], actual[2])

	if history == 0 {
		counts[backend] = actual
	} else if actual != counts[backend] {
		t.Fatalf("unrelated history grew affected mutation work: %v -> %v", counts[backend], actual)
	}
}

func seedUnrelatedMutationHistory(t *testing.T, owner *Owner, scope memory.Scope, history int) {
	t.Helper()

	if err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		for index := range history {
			key, _ := privacyKey(scope, Operation, "unrelated:"+strconv.Itoa(index))
			if err := putPrivacyRecord(ctx, tx, key, Operation, true); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
