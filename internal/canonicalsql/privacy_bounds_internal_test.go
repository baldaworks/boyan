package canonicalsql

import (
	"context"
	"errors"
	"fmt"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func privacyRecordState(t *testing.T, owner *Owner, h privacyHistory) sourcePrivacy {
	t.Helper()

	var result sourcePrivacy

	if err := owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
		var err error

		result, _, err = loadSourcePrivacy(ctx, tx, h.source)

		return err
	}); err != nil {
		t.Fatal(err)
	}

	return result
}

func TestPrivacyStickyUnknownAndExactScope(t *testing.T) {
	forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
		h := newPrivacyHistory(t)
		seedPrivacyHistory(t, owner, h)
		other := h
		other.scope.Kind = memory.ScopeKindGroup
		other.source.Scope = other.scope
		other.item.Scope = other.scope
		seedPrivacyHistory(t, owner, other)

		if err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
			if err := putPrivacyScope(ctx, tx, h.scope, true); err != nil {
				return err
			}

			return putPrivacyScope(ctx, tx, h.scope, false)
		}); err != nil {
			t.Fatal(err)
		}

		if err := owner.PrepareSourcePrivacy(t.Context()); err != nil {
			t.Fatal(err)
		}

		if got := privacyRecordState(t, owner, h); got.State != "unknown" || !got.StickyUnknown {
			t.Fatalf("incomplete scope not sticky: %+v", got)
		}

		if got := privacyRecordState(t, owner, other); got.State != "standard" {
			t.Fatalf("foreign scope tainted: %+v", got)
		}

		if err := owner.Update(t.Context(),
			func(ctx context.Context,
				tx *Tx) error {
				return tx.Delete(ctx,
					Key{Family: SourcePrivacyReady})
			}); err != nil {
			t.Fatal(err)
		}

		if err := owner.PrepareSourcePrivacy(t.Context()); err != nil {
			t.Fatal(err)
		}

		if got := privacyRecordState(t, owner, h); got.State != "unknown" {
			t.Fatalf("completed unknown laundered: %+v", got)
		}
	})
}

func TestPrivacyMembershipBound(t *testing.T) {
	forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
		h := newPrivacyHistory(t)
		for i := len(h.messages); i < 33; i++ {
			message := h.messages[0]
			message.MessageID = fmt.Sprintf("message-%02d", i)
			h.messages = append(h.messages, message)
		}

		seedPrivacyHistory(t, owner, h)

		if err := owner.PrepareSourcePrivacy(t.Context()); err != nil {
			t.Fatal(err)
		}

		record := privacyRecordState(t, owner, h)
		if record.State != "unknown" || len(record.MessageIDs) != 32 || record.MessageMembershipComplete {
			t.Fatalf("overflow authorized: %+v", record)
		}
	})
}

func TestPrivacyPartialPageRestartAndMarkerLast(t *testing.T) {
	forEachOwner(t, func(t *testing.T, owner *Owner, reopen func() *Owner) {
		h := newPrivacyHistory(t)
		seedPrivacyHistory(t, owner, h)

		growSQLPrivacyHistory(t, owner, h)

		interruptPrivacyPage(t, owner)

		assertCode(t, owner.SourcePrivacyReady(t.Context()), memory.CodeUnavailable)

		if err := owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
			rows, err := tx.Scan(ctx, Range{Family: SourcePrivacy, Limit: 100})
			if len(rows) != 32 {
				t.Fatalf("partial page committed %d records", len(rows))
			}

			return err
		}); err != nil {
			t.Fatal(err)
		}

		if err := owner.Close(); err != nil {
			t.Fatal(err)
		}

		owner = reopen()
		assertCode(t, owner.SourcePrivacyReady(t.Context()), memory.CodeUnavailable)

		if err := owner.PrepareSourcePrivacy(t.Context()); err != nil {
			t.Fatal(err)
		}

		if got := privacyRecordState(t, owner, h); got.State != "standard" {
			t.Fatalf("partial preparation became sticky: %+v", got)
		}

		requireProofSuccess(t, owner.SourcePrivacyReady(t.Context()))
	})
}

func growSQLPrivacyHistory(t *testing.T, owner *Owner, h privacyHistory) {
	t.Helper()

	if err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		for i := 0; i < 65; i++ {
			source := h.source
			source.SourceID = fmt.Sprintf("history-%02d", i)

			key, _ := privacyKey(h.scope, Source, source.SourceID)
			if err := putPrivacyRecord(ctx, tx, key, Source, source); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func interruptPrivacyPage(t *testing.T, owner *Owner) {
	t.Helper()

	ctx, release, err := owner.admit(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	count := 0
	stop := errors.New("interrupt page")
	err = owner.preparePrivacyFamily(ctx,
		Source,
		func(ctx context.Context,
			tx *Tx,
			key Key,
			scope memory.Scope,
			id string) error {
			count++
			if count == 33 {
				return stop
			}

			return initializeHistoricalSourcePrivacy(ctx, tx, key, scope, id)
		})

	release()

	if !errors.Is(err, stop) {
		t.Fatalf("interrupt lost: %v", err)
	}
}
