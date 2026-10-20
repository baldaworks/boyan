//go:build integration

package canonicalsql

import (
	"context"
	"errors"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"testing"
)

func TestTraceFreshAuthorityInterleavings(t *testing.T) {
	for _, stage := range []string{"preflight-hydration", "hydration-output"} {
		t.Run(stage, func(t *testing.T) {
			for _, kind := range []string{"source-denial", "revision-denial", "scope-change", "sensitive",
				"head-change", "canceled", "unchanged", "membership-extension", "raw-content", "head-corruption", "evidence-role"} {
				t.Run(kind, func(t *testing.T) {
					forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) { runTraceInterleaving(t, owner, stage, kind) })
				})
			}
		})
	}
}

func runTraceInterleaving(t *testing.T, owner *Owner, stage, kind string) {
	t.Helper()

	h := prepareTraceHistory(t, owner)

	request := memory.TraceRequest{Scope: h.scope, Root: memory.RevisionRef{ItemID: h.item.ItemID,
		RevisionID: h.revision.RevisionID}, MaxNodes: 10}

	graph, err := owner.preflightTraceGraph(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}

	hydrate := func(ctx context.Context) error {
		return owner.View(ctx, func(ctx context.Context, tx *Tx) error { return hydrateTraceGraph(ctx, tx, request, &graph) })
	}
	if stage == "hydration-output" {
		if err := hydrate(t.Context()); err != nil {
			t.Fatal(err)
		}

		response, err := buildTraceResponse(request, graph)
		if err != nil || len(response.Sources) != 1 {
			t.Fatalf("hydrated graph=%+v err=%v", response, err)
		}
	}

	alterReaderBarrier(t, owner, h, kind)
	alterTraceBarrier(t, owner, h, kind)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	if kind == "canceled" {
		cancel()
	}

	if stage == "preflight-hydration" {
		err = hydrate(ctx)
	} else {
		err = owner.finalTracePrivacyBarrier(ctx, graph)
	}

	assertTraceInterleaving(t, kind, err)
}

func assertTraceInterleaving(t *testing.T, kind string, err error) {
	t.Helper()

	if kind == "unchanged" {
		if err != nil {
			t.Fatal(err)
		}

		return
	}

	if err == nil {
		t.Fatal("obsolete snapshot authorized output")
	}

	if kind == "canceled" && !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}

	if kind == "head-corruption" {
		code, class, _ := memory.ClassifyError(err)
		if code != memory.CodeStoreFailure || class != memory.ErrorClassPermanent {
			t.Fatalf("masked storage error=%v", err)
		}
	}
}

func alterTraceBarrier(t *testing.T, owner *Owner, h privacyHistory, kind string) {
	t.Helper()

	if err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		switch kind {
		case "membership-extension", "evidence-role":
			message := h.messages[0]
			if kind == "membership-extension" {
				message.MessageID = "extra"
			} else {
				message.Role = memory.MessageRoleAssistant
			}

			key, _ := privacyKey(h.scope, Message, message.MessageID)

			return putPrivacyRecord(ctx, tx, key, Message, message)
		case "raw-content":
			key, _ := payloadRecordKey(h.messages[1].Payload)

			return putPrivacyRecord(ctx, tx, key, Payload, []byte("corrupted raw content"))
		case "head-corruption":
			key, _ := privacyKey(h.scope, Head, h.item.ItemID)

			return putPrivacyRecord(ctx, tx, key, Head, memory.ItemHead{ItemID: "wrong", RevisionID: h.revision.RevisionID})
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTraceReadRemainsCompleteAfterFinalBarrier(t *testing.T) {
	forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
		if err := owner.PrepareSourcePrivacy(t.Context()); err != nil {
			t.Fatal(err)
		}

		h := newPrivacyHistory(t)
		if _, err := owner.ApplyCanonicalMutation(t.Context(), historyMutation(h)); err != nil {
			t.Fatal(err)
		}

		r := memory.TraceRequest{Scope: h.scope, Root: memory.RevisionRef{ItemID: h.item.ItemID,
			RevisionID: h.revision.RevisionID}, MaxNodes: 10}

		response, err := owner.Trace(t.Context(), r)
		if err != nil {
			t.Fatal(err)
		}

		alterReaderBarrier(t, owner, h, "source-denial")

		if len(response.Sources) != 1 || response.Sources[0].Turn.Messages[0].Text == "" {
			t.Fatal("completed read changed")
		}

		next, err := owner.Trace(t.Context(), r)
		if err == nil || len(next.Sources) != 0 || len(next.Revisions) != 0 {
			t.Fatalf("later denied read=%+v %v", next, err)
		}
	})
}

func prepareTraceHistory(t *testing.T, owner *Owner) privacyHistory {
	t.Helper()

	if err := owner.PrepareSourcePrivacy(t.Context()); err != nil {
		t.Fatal(err)
	}

	h := newPrivacyHistory(t)
	if _, err := owner.ApplyCanonicalMutation(t.Context(), historyMutation(h)); err != nil {
		t.Fatal(err)
	}

	return h
}
