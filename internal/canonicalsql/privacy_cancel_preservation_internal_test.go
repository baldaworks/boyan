package canonicalsql

import (
	"context"
	"errors"
	"fmt"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestPrivacyRevisionPageCancellationRestarts(t *testing.T) {
	forEachOwner(t, func(t *testing.T, owner *Owner, reopen func() *Owner) {
		h := newPrivacyHistory(t)
		seedPrivacyHistory(t, owner, h)
		seedPrivacyRevisionPages(t, owner, h)
		cancelPrivacyRevisionPage(t, owner)
		err := owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
			edges, err := tx.Scan(ctx, Range{Family: SourceRevision, Scope: &h.scope, Limit: 100})
			if err == nil && len(edges) != sourcePrivacyBatchSize {
				t.Fatalf("committed revision page: edges=%d want=%d", len(edges), sourcePrivacyBatchSize)
			}

			return err
		})
		requireEraseTestNoError(t, err)
		assertCode(t, owner.SourcePrivacyReady(t.Context()), memory.CodeUnavailable)
		requireEraseTestNoError(t, owner.Close())
		owner = reopen()
		assertCode(t, owner.SourcePrivacyReady(t.Context()), memory.CodeUnavailable)
		requireEraseTestNoError(t, owner.PrepareSourcePrivacy(t.Context()))
		assertPrivacyHistory(t, owner, h, "standard")
		requireEraseTestNoError(t, owner.SourcePrivacyReady(t.Context()))
	})
}

func seedPrivacyRevisionPages(t *testing.T, owner *Owner, h privacyHistory) {
	t.Helper()

	err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		for i := range 65 {
			revision := h.revision
			revision.RevisionID = fmt.Sprintf("history-%02d", i)

			key, _ := privacyKey(h.scope, Revision, revision.RevisionID)
			if err := putPrivacyRecord(ctx, tx, key, Revision, revision); err != nil {
				return err
			}
		}

		return nil
	})
	requireEraseTestNoError(t, err)
}

func cancelPrivacyRevisionPage(t *testing.T, owner *Owner) {
	t.Helper()

	requested, cancel := context.WithCancel(t.Context())
	defer cancel()

	ctx, release, err := owner.admit(requested)
	requireEraseTestNoError(t, err)

	defer release()

	requireEraseTestNoError(t, owner.preparePrivacyFamily(ctx, Source, initializeHistoricalSourcePrivacy))
	requireEraseTestNoError(t, owner.preparePrivacyFamily(ctx, Message, collectHistoricalPrivacyMessage))

	visited := 0

	err = owner.preparePrivacyFamily(ctx, Revision,
		func(ctx context.Context, tx *Tx, key Key, scope memory.Scope, id string) error {
			// The first page has committed when the second page's first visitor
			// starts. Cancel before visiting it: no second-page effects survive.
			if visited == sourcePrivacyBatchSize {
				cancel()

				return ctx.Err()
			}

			visited++

			return collectHistoricalPrivacyRevision(ctx, tx, key, scope, id)
		})
	if !errors.Is(err, context.Canceled) || visited != sourcePrivacyBatchSize {
		t.Fatalf("revision page cancellation: visited=%d err=%v", visited, err)
	}
}
