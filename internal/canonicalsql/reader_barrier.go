package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"reflect"
)

type readerSnapshot struct {
	state     memory.ScopeState
	records   []memory.RecallRecord
	revisions map[string]memory.MemoryRevision
	sources   map[string]memory.CanonicalSourceRecord
}

// No payload reads follow this fresh snapshot. A denial committed since
// hydration cannot be authorized by the earlier snapshot; a changed canonical
// scope retries rather than combining records from different scope versions.
func (o *Owner) finalReaderBarrier(ctx context.Context, captured readerSnapshot) error {
	return o.View(ctx, func(ctx context.Context, tx *Tx) error {
		state, err := readerScopeState(ctx, tx, captured.state.Scope)
		if err != nil {
			return err
		}

		if state.Version != captured.state.Version || state.ChangeSeq != captured.state.ChangeSeq {
			return readerStateChanged()
		}

		incomplete, err := privacyScopeIncomplete(ctx, tx, state.Scope)
		if err != nil {
			return err
		}

		if incomplete && len(captured.records) > 0 {
			return readerDenied()
		}

		if err := recheckReaderSources(ctx, tx, captured); err != nil {
			return err
		}

		if err := recheckReaderRevisions(ctx, tx, captured); err != nil {
			return err
		}

		return ctx.Err()
	})
}

func recheckReaderSources(ctx context.Context, tx *Tx, captured readerSnapshot) error {
	for id, previous := range captured.sources {
		source, found, err := readerSource(ctx, tx, captured.state.Scope, id)
		if err != nil {
			return err
		}

		if !found {
			return readerDenied()
		}

		if source != previous {
			return readerStateChanged()
		}
	}

	return nil
}

func recheckReaderRevisions(ctx context.Context, tx *Tx, captured readerSnapshot) error {
	scope := captured.state.Scope
	for id, previous := range captured.revisions {
		denied, err := privacyDenied(ctx, tx, scope, DeniedRevision, id)
		if err != nil {
			return err
		}

		if denied {
			return readerDenied()
		}

		revision, err := loadProofRevision(ctx, tx, scope, id)
		if err != nil {
			return err
		}

		if !reflect.DeepEqual(revision, previous) {
			return readerStateChanged()
		}
	}

	for _, record := range captured.records {
		if err := requireProofHead(ctx, tx, scope, memory.RevisionRef{ItemID: record.ItemID,
			RevisionID: record.RevisionID}); err != nil {
			return readerStateChanged()
		}
	}

	return nil
}

func readerStateChanged() error {
	return memory.RetryableError(memory.CodeConflict, "canonical reader state changed", nil)
}
func readerDenied() error {
	return memory.PermanentError(memory.CodeForgotten, "canonical reader content is unavailable", nil)
}
