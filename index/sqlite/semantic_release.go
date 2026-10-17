//nolint:lll // Fixed SQL keeps the full captured identity and finite keyset predicates auditable.
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	semantic "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

// ReleaseUnpublished releases only the trusted caller's exact generated BuildID.
// It reserves before waiting for the scope, revokes completeness atomically, and
// then removes finite pages. A failure never reports logical removal completed.
//
//nolint:cyclop // Lease, reservation, scope and revocation each have an explicit failure path.
func (store *Store) ReleaseUnpublished(ctx context.Context, scope memory.Scope, buildID string) (released semantic.CandidateRelease, releaseErr error) {
	if ctx == nil || semantic.ValidateScope(scope) != nil || !semantic.ValidProjectionIdentity(buildID) || !store.embeddingsEnabled {
		return semantic.CandidateRelease{}, semantic.ErrInvalid
	}

	operation, done, err := store.acquire(ctx)
	if err != nil {
		return semantic.CandidateRelease{}, semanticStorageError(err)
	}
	defer done()
	// A later page may observe closing before its nested write lease sees
	// cancellation. Preserve the admitted operation's cancellation identity.
	defer func() {
		if releaseErr != nil && operation.Err() != nil {
			releaseErr = operation.Err()
		}
	}()

	if _, loaded := store.semanticStaging.LoadOrStore(buildID, semanticOperationRelease); loaded {
		return semantic.CandidateRelease{}, semantic.ErrConflict
	}
	defer store.semanticStaging.Delete(buildID)

	unlock, err := store.lockScope(operation, scope)
	if err != nil {
		return semantic.CandidateRelease{}, semanticStorageError(err)
	}
	defer unlock()

	var (
		identity semanticReleaseIdentity
		result   semantic.CandidateRelease
	)

	err = store.write(operation, func(ctx context.Context, tx *sql.Tx) error {
		var checkErr error

		identity, result, checkErr = store.revokeSemanticCandidate(ctx, tx, scope, buildID)

		return checkErr
	})
	if err != nil {
		return semantic.CandidateRelease{}, semanticStorageError(err)
	}

	if result.Disposition != "" {
		return result, nil
	}

	if err = store.deleteSemanticCandidate(operation, identity); err != nil {
		return semantic.CandidateRelease{}, semanticStorageError(err)
	}

	return semantic.CandidateRelease{Disposition: semantic.CandidateReleased}, nil
}

type semanticReleaseIdentity struct {
	rowid int64
	state semantic.State
}

//nolint:cyclop // Missing, wrong scope, active and unpublished states must remain distinct.
func (store *Store) revokeSemanticCandidate(ctx context.Context, tx *sql.Tx, scope memory.Scope, build string) (semanticReleaseIdentity, semantic.CandidateRelease, error) {
	var (
		identity semanticReleaseIdentity
		owns     bool
	)

	err := tx.QueryRowContext(ctx, "SELECT rowid,scope_kind=? AND scope_key=? FROM semantic_build WHERE build_id=?", string(scope.Kind), scope.Key, build).Scan(&identity.rowid, &owns)
	if errors.Is(err, sql.ErrNoRows) {
		return identity, semantic.CandidateRelease{Disposition: semantic.CandidateMissing}, nil
	}

	if err != nil {
		return identity, semantic.CandidateRelease{}, err
	}

	if !owns {
		return identity, semantic.CandidateRelease{}, semantic.ErrConflict
	}

	if err = store.prepareSemanticRelease(ctx, tx, scope, build, identity.rowid); err != nil {
		return identity, semantic.CandidateRelease{}, err
	}

	state, complete, err := readSemanticState(ctx, tx, build)
	if err != nil {
		return identity, semantic.CandidateRelease{}, err
	}

	var activeCount int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM semantic_active WHERE build_id=?", build).Scan(&activeCount); err != nil {
		return identity, semantic.CandidateRelease{}, err
	}

	if activeCount != 0 {
		active, found, activeErr := activeSemanticState(ctx, tx, scope)
		if activeErr != nil {
			return identity, semantic.CandidateRelease{}, activeErr
		}

		if activeCount != 1 || !found || active != state || !complete || state.PublicationEpoch == 0 {
			return identity, semantic.CandidateRelease{}, semantic.ErrCorrupt
		}

		return identity, semantic.CandidateRelease{Disposition: semantic.CandidatePublished, Epoch: state.PublicationEpoch}, nil
	}

	if state.PublicationEpoch != 0 {
		return identity, semantic.CandidateRelease{}, semantic.ErrConflict
	}

	if state.Status != semantic.StatusReady {
		return identity, semantic.CandidateRelease{}, semantic.ErrCorrupt
	}

	identity.state = state
	_, err = tx.ExecContext(ctx, `UPDATE semantic_build SET complete=0
 WHERE rowid=? AND build_id=? AND scope_kind=? AND scope_key=? AND epoch=?
 AND NOT EXISTS(SELECT 1 FROM semantic_active WHERE build_id=?)`, identity.rowid, build, string(scope.Kind), scope.Key, encodeUint64(0), build)

	return identity, semantic.CandidateRelease{}, err
}

func (store *Store) prepareSemanticRelease(ctx context.Context, tx *sql.Tx, scope memory.Scope, build string, rowid int64) error {
	if operation, live := store.semanticStaging.Load(build); !live || operation != semanticOperationRelease {
		return semantic.ErrConflict
	}

	// Privacy revocation retains the candidate's original metadata and safe
	// remainder, unlike a public degraded marker. Restore only this exact owner's
	// incomplete staging shape so the strict metadata and per-page checks apply.
	// Completeness remains revoked; a failed metadata check rolls this back too.
	_, err := tx.ExecContext(ctx, `UPDATE semantic_build SET status='building', reason=''
 WHERE rowid=? AND build_id=? AND scope_kind=? AND scope_key=? AND epoch=? AND complete=0
 AND status='degraded' AND reason='canonical_forget'
 AND NOT EXISTS(SELECT 1 FROM semantic_active WHERE build_id=?)`, rowid, build, string(scope.Kind), scope.Key, encodeUint64(0), build)

	return err
}

func (store *Store) checkSemanticRelease(ctx context.Context, tx *sql.Tx, identity semanticReleaseIdentity) error {
	state := identity.state
	if operation, live := store.semanticStaging.Load(state.BuildID); !live || operation != semanticOperationRelease {
		return semantic.ErrConflict
	}

	var matches bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM semantic_build b
 WHERE b.rowid=? AND b.build_id=? AND b.scope_kind=? AND b.scope_key=? AND b.epoch=? AND b.complete=0
 AND NOT EXISTS(SELECT 1 FROM semantic_active a WHERE a.build_id=b.build_id))`, identity.rowid, state.BuildID, string(state.Scope.Kind), state.Scope.Key, encodeUint64(0)).Scan(&matches); err != nil {
		return err
	}

	if !matches {
		return semantic.ErrConflict
	}

	current, _, err := readSemanticState(ctx, tx, state.BuildID)
	if err != nil {
		return err
	}

	if current != state {
		return semantic.ErrCorrupt
	}

	return nil
}

// Each transaction deletes at most 128 keys from one owned family. Membership
// cascades are cooperative SQL work, not a physical-byte or latency quota.
func (store *Store) deleteSemanticCandidate(ctx context.Context, identity semanticReleaseIdentity) error {
	for _, query := range []string{
		`DELETE FROM semantic_chunk WHERE build_id=? AND rowid IN (SELECT rowid FROM semantic_chunk WHERE build_id=? AND rowid>? ORDER BY rowid LIMIT 128) RETURNING rowid`,
		`DELETE FROM semantic_coverage WHERE build_id=? AND rowid IN (SELECT rowid FROM semantic_coverage WHERE build_id=? AND rowid>? ORDER BY rowid LIMIT 128) RETURNING rowid`,
	} {
		var after int64

		for {
			var count int

			var err error

			after, count, err = store.deleteSemanticCandidatePage(ctx, identity, query, after)
			if err != nil {
				return err
			}

			if count == 0 {
				break
			}
		}
	}

	return store.write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := store.checkSemanticRelease(ctx, tx, identity); err != nil {
			return err
		}

		state := identity.state

		result, err := tx.ExecContext(ctx, `DELETE FROM semantic_build WHERE rowid=? AND build_id=? AND scope_kind=? AND scope_key=? AND epoch=? AND complete=0
 AND NOT EXISTS(SELECT 1 FROM semantic_active WHERE build_id=?)
 AND NOT EXISTS(SELECT 1 FROM semantic_chunk WHERE build_id=?)
 AND NOT EXISTS(SELECT 1 FROM semantic_coverage WHERE build_id=?)`, identity.rowid, state.BuildID, string(state.Scope.Kind), state.Scope.Key, encodeUint64(0), state.BuildID, state.BuildID, state.BuildID)
		if err != nil {
			return err
		}

		count, err := result.RowsAffected()
		if err == nil && count != 1 {
			return semantic.ErrConflict
		}

		return err
	})
}

func (store *Store) deleteSemanticCandidatePage(ctx context.Context, identity semanticReleaseIdentity,
	query string, after int64) (int64, int, error) {
	var count int

	err := store.write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := store.checkSemanticRelease(ctx, tx, identity); err != nil {
			return err
		}

		rows, err := tx.QueryContext(ctx, query, identity.state.BuildID, identity.state.BuildID, after)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var rowid int64
			if err = rows.Scan(&rowid); err != nil {
				return err
			}

			after = max(after, rowid)
			count++
		}

		return rows.Err()
	})

	return after, count, err
}
