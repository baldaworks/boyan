package canonicalsql

import (
	"context"
	"fmt"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"strings"
	"testing"
)

func TestTracePreparedRepresentations(t *testing.T) {
	for _, kind := range []string{"standard", "certified", "embedded", "embedded-only", "opaque", "sensitive",
		"membership-overflow"} {
		t.Run(kind, func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, reopen func() *Owner) { runTracePrepared(t, owner, reopen, kind) })
		})
	}
}

func runTracePrepared(t *testing.T, owner *Owner, reopen func() *Owner, kind string) {
	t.Helper()
	h := newPrivacyHistory(t)
	configurePrivacyCase(&h, kind)
	configureEmbeddedPrivacy(t, &h, kind)

	if kind == "membership-overflow" {
		for i := len(h.messages); i < 33; i++ {
			message := h.messages[0]
			message.MessageID = fmt.Sprintf("extra-%02d", i)
			h.messages = append(h.messages, message)
		}
	}

	seedPrivacyHistory(t, owner, h)

	want := "standard"
	writePrivacyRepresentation(t, owner, h, kind, &want)

	if err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		key, _ := privacyKey(h.scope, Head, h.item.ItemID)

		return putPrivacyRecord(ctx, tx, key, Head, memory.ItemHead{ItemID: h.item.ItemID, RevisionID: h.revision.RevisionID})
	}); err != nil {
		t.Fatal(err)
	}

	if err := owner.PrepareSourcePrivacy(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}

	owner = reopen()
	request := memory.TraceRequest{Scope: h.scope, Root: memory.RevisionRef{ItemID: h.item.ItemID,
		RevisionID: h.revision.RevisionID}, MaxNodes: 10}
	got, err := owner.Trace(t.Context(), request)

	assertPreparedTraceResult(t, kind, got, err)
}

func assertPreparedTraceResult(t *testing.T, kind string, got memory.TraceResponse, err error) {
	t.Helper()

	denied := kind == "opaque" || kind == "sensitive" || kind == "membership-overflow"
	if denied {
		if err == nil || len(got.Sources) != 0 {
			t.Fatalf("unproven history=%+v err=%v", got, err)
		}

		return
	}

	if err != nil || len(got.Sources) != 1 || len(got.Sources[0].Turn.Messages) != 2 {
		t.Fatalf("prepared=%+v err=%v", got, err)
	}
}

type traceNoPayloadQuery struct{ base query }

func (q traceNoPayloadQuery) exec(ctx context.Context, statement string, args ...any) (int64, error) {
	return q.base.exec(ctx, statement, args...)
}

func (q traceNoPayloadQuery) query(ctx context.Context, statement string, args ...any) (rows, error) {
	if strings.Contains(statement, "FROM payload ") || strings.Contains(statement, "FROM boyan_canonical.payload ") {
		return nil, memory.PermanentError(memory.CodeStoreFailure, "preflight read raw payload", nil)
	}

	return q.base.query(ctx, statement, args...)
}

func TestTracePreflightRejectsBeforeRawPayload(t *testing.T) {
	for _, kind := range []string{"node-limit", "sensitive", "source-denial", "opaque"} {
		t.Run(kind, func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
				h := prepareTracePreflightHistory(t, owner, kind)
				alterReaderBarrier(t, owner, h, kind)

				request := memory.TraceRequest{Scope: h.scope, Root: memory.RevisionRef{ItemID: h.item.ItemID,
					RevisionID: h.revision.RevisionID}, MaxNodes: 10}
				if kind == "node-limit" {
					request.MaxNodes = 1
				}

				err := owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
					tx.q = traceNoPayloadQuery{tx.q}
					graph := traceGraph{captured: readerSnapshot{sources: make(map[string]memory.CanonicalSourceRecord),
						revisions: make(map[string]memory.MemoryRevision)}}

					return collectTraceClosure(ctx, tx, request, &graph)
				})
				code, _, _ := memory.ClassifyError(err)

				want := memory.CodeForgotten
				if kind == "node-limit" {
					want = memory.CodeLimitExceeded
				}

				if code != want {
					t.Fatalf("preflight=%v want=%s", err, want)
				}
			})
		})
	}
}
