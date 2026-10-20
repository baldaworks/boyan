//go:build integration

package canonicalsql

import (
	"context"
	"errors"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"testing"
	"time"
)

func TestReaderFinalBarrierRejectsObsoleteSnapshot(t *testing.T) {
	for _, kind := range []string{"source-denial", "revision-denial", "scope-change", "sensitive",
		"head-change", "canceled", "unchanged"} {
		t.Run(kind, func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
				runReaderBarrierCase(t, owner, kind)
			})
		})
	}
}

func alterReaderBarrier(t *testing.T, owner *Owner, h privacyHistory, kind string) {
	t.Helper()

	if err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		switch kind {
		case "source-denial":
			key, _ := privacyKey(h.scope, DeniedSource, h.source.SourceID)

			return putPrivacyRecord(ctx, tx, key, DeniedSource, deniedSource{SourceID: h.source.SourceID, DeniedAt: time.Now()})
		case "revision-denial":
			key, _ := privacyKey(h.scope, DeniedRevision, h.revision.RevisionID)

			return putPrivacyRecord(ctx, tx, key, DeniedRevision, deniedRevision{RevisionID: h.revision.RevisionID,
				DeniedAt: time.Now()})
		case "scope-change":
			key, _ := proofScopeKey(h.scope)

			state, err := loadProofScopeState(ctx, tx, h.scope, key)
			if err != nil {
				return err
			}

			state.Version++

			return putPrivacyRecord(ctx, tx, key, ScopeState, state)
		case "sensitive":
			source := h.source
			source.Sensitivity = memory.SensitivitySensitive
			key, _ := privacyKey(h.scope, Source, source.SourceID)

			return putPrivacyRecord(ctx, tx, key, Source, source)
		case "head-change":
			key, _ := privacyKey(h.scope, Head, h.item.ItemID)

			return putPrivacyRecord(ctx, tx, key, Head, memory.ItemHead{ItemID: h.item.ItemID, RevisionID: "other"})
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func runReaderBarrierCase(t *testing.T, owner *Owner, kind string) {
	t.Helper()

	if err := owner.PrepareSourcePrivacy(t.Context()); err != nil {
		t.Fatal(err)
	}

	h := newPrivacyHistory(t)
	if _, err := owner.ApplyCanonicalMutation(t.Context(), historyMutation(h)); err != nil {
		t.Fatal(err)
	}

	var captured readerSnapshot

	if err := owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
		records, err := loadReaderRecords(ctx, tx, h.scope, []string{h.revision.RevisionID}, time.Now(), &captured)
		if err == nil && len(records) != 1 {
			t.Fatal("hydration did not capture real content")
		}

		return err
	}); err != nil {
		t.Fatal(err)
	}
	// This is a retained native denial fixture between two real snapshots,
	// not an implementation of the public erase writer owned by Task 12.
	alterReaderBarrier(t, owner, h, kind)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	if kind == "canceled" {
		cancel()
	}

	err := owner.finalReaderBarrier(ctx, captured)
	assertReaderBarrierResult(t, kind, err)
}

func assertReaderBarrierResult(t *testing.T, kind string, err error) {
	t.Helper()

	if kind == "unchanged" {
		if err != nil {
			t.Fatal(err)
		}

		return
	}

	if kind == "canceled" {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel=%v", err)
		}

		return
	}

	code, _, _ := memory.ClassifyError(err)

	want := memory.CodeForgotten
	if kind == "scope-change" || kind == "head-change" {
		want = memory.CodeConflict
	}

	if code != want {
		t.Fatalf("barrier=%v want=%s", err, want)
	}
}
