//nolint:lll // Fixed SQL statements keep complete CAS identities auditable together.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	semantic "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

// Publish atomically activates a complete staged generation and retires its predecessor.
//
//nolint:cyclop // Candidate, source, epoch and exact logical budget are independent rejection gates.
func (store *Store) Publish(ctx context.Context, publication semantic.Publication) error {
	if ctx == nil || !store.embeddingsEnabled || semantic.ValidateView(publication.View) != nil ||
		!semantic.ValidProjectionIdentity(publication.BuildID) || !validSemanticExpectation(publication.ExpectedActiveBuildID, publication.ExpectedActiveEpoch) {
		return semantic.ErrInvalid
	}

	return semanticStorageError(store.semanticScopeWrite(ctx, publication.Scope, func(ctx context.Context, tx *sql.Tx) error {
		previous, epoch, err := semanticPublicationEpoch(ctx, tx, publication.Scope, publication.ExpectedActiveBuildID, publication.ExpectedActiveEpoch)
		if err != nil {
			return err
		}

		state, complete, err := readSemanticState(ctx, tx, publication.BuildID)
		if err != nil {
			return err
		}

		if !complete || state.Status != semantic.StatusReady || state.PublicationEpoch != 0 || !semantic.StateMatchesView(state, publication.View) {
			return semantic.ErrConflict
		}

		if _, err = validateSemanticBuild(ctx, tx, state.BuildID, nil); err != nil {
			return err
		}

		if err = recheckSemanticSource(ctx, tx, state); err != nil {
			return err
		}

		encoded, err := semantic.EncodeStoredState(semantic.StoredProjectionState{SchemaVersion: semantic.ProjectionSchemaVersion, Complete: true, State: state})
		if err != nil {
			return err
		}

		controlBytes := uint64(semantic.LogicalStateKeyBytes + len(encoded))
		if state.EncodedBytes < controlBytes {
			return semantic.ErrCorrupt
		}

		state.PublicationEpoch = epoch

		state, err = semantic.FinalizeEncodedBytes(state, state.EncodedBytes-controlBytes)
		if err != nil {
			return err
		}

		if _, err = tx.ExecContext(ctx, "UPDATE semantic_build SET epoch=?,encoded_bytes=? WHERE build_id=?", encodeUint64(epoch), state.EncodedBytes, state.BuildID); err != nil {
			return err
		}

		return switchSemanticPublication(ctx, tx, state, previous)
	}))
}

// PublishDegraded activates a safe diagnostic marker and atomically retires
// the exact predecessor using the same source and pointer checks as Publish.
//
//nolint:cyclop,funlen // Safe marker admission and CAS/source checks must precede all mutations.
func (store *Store) PublishDegraded(ctx context.Context, marker semantic.Degraded, expectedBuildID string) (semantic.State, error) {
	if ctx == nil || !store.embeddingsEnabled || semantic.ValidateGeneration(marker.Generation) != nil || !marker.SafeReason.Valid() || !validSemanticExpectation(expectedBuildID, marker.ExpectedActiveEpoch) {
		return semantic.State{}, semantic.ErrInvalid
	}

	var result semantic.State

	err := store.semanticScopeWrite(ctx, marker.Generation.Scope, func(ctx context.Context, tx *sql.Tx) error {
		previous, epoch, err := semanticPublicationEpoch(ctx, tx, marker.Generation.Scope, expectedBuildID, marker.ExpectedActiveEpoch)
		if err != nil {
			return err
		}

		state := semantic.State{
			SchemaVersion:       semantic.ProjectionSchemaVersion,
			Scope:               marker.Generation.Scope,
			BuildID:             marker.Generation.BuildID,
			LexicalGenerationID: marker.Generation.LexicalGenerationID,
			LexicalWatermark:    marker.Generation.LexicalWatermark,
			ModelSpace:          marker.Generation.ModelSpace,
			Dimensions:          marker.Generation.Dimensions,
			Status:              semantic.StatusDegraded,
			SafeReason:          string(marker.SafeReason),
			PublicationEpoch:    epoch,
			CoverageHash:        semantic.CoverageFingerprint(nil),
			Omitted:             marker.Generation.Omitted,
			ReadyAt:             marker.Generation.ReadyAt.UTC(),
		}

		state, err = semantic.FinalizeEncodedBytes(state, 0)
		if err != nil {
			return err
		}

		physical, err := semanticPhysicalSource(ctx, tx, state)
		if err != nil {
			return err
		}

		var exists bool
		if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM semantic_build WHERE build_id=?)", state.BuildID).Scan(&exists); err != nil {
			return err
		}

		if exists {
			return semantic.ErrConflict
		}

		const insert = `INSERT INTO semantic_build(build_id,scope_kind,scope_key,generation_id,physical_build_id,
 watermark,model_space,dimensions,epoch,complete,status,reason,coverage_hash,encoded_bytes,
 omitted_ineligible,omitted_expired,omitted_sensitive,ready_at) VALUES(?,?,?,?,?,?,?,?,?,1,'degraded',?,?,?,?,?,?,?)`
		if _, err = tx.ExecContext(ctx, insert, state.BuildID, string(state.Scope.Kind), state.Scope.Key,
			state.LexicalGenerationID, physical, encodeUint64(state.LexicalWatermark), state.ModelSpace, state.Dimensions,
			encodeUint64(epoch), state.SafeReason, state.CoverageHash, state.EncodedBytes, state.Omitted.Ineligible,
			state.Omitted.Expired, state.Omitted.Sensitive, encodeTime(state.ReadyAt)); err != nil {
			return err
		}

		if err = switchSemanticPublication(ctx, tx, state, previous); err != nil {
			return err
		}

		result = state

		return nil
	})
	if err != nil {
		return semantic.State{}, semanticStorageError(err)
	}

	return result, nil
}

// InvalidatePublication retires only the specified active publication, leaving
// safe degraded control metadata from which a replacement can obtain its CAS.
//

func (store *Store) InvalidatePublication(ctx context.Context, scope memory.Scope, buildID string, epoch uint64) error {
	if ctx == nil || !store.embeddingsEnabled || semantic.ValidateScope(scope) != nil || !semantic.ValidProjectionIdentity(buildID) || epoch == 0 {
		return semantic.ErrInvalid
	}

	return semanticStorageError(store.semanticScopeWrite(ctx, scope, func(ctx context.Context, tx *sql.Tx) error {
		var matches bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM semantic_active WHERE scope_kind=? AND scope_key=? AND build_id=? AND epoch=?)`, string(scope.Kind), scope.Key, buildID, encodeUint64(epoch)).Scan(&matches); err != nil {
			return err
		}

		if !matches {
			return semantic.ErrConflict
		}

		state, err := validateSemanticRetirement(ctx, tx, scope, buildID, epoch)
		if err != nil {
			return err
		}

		return degradeSemanticState(ctx, tx, state)
	}))
}

func degradeSemanticState(ctx context.Context, tx *sql.Tx, state semantic.State) error {
	state.Status = semantic.StatusDegraded
	state.SafeReason = string(semantic.ReasonUnavailable)
	state.ChunkCount = 0
	state.RecordCount = 0
	state.CoverageHash = semantic.CoverageFingerprint(nil)

	state, err := semantic.FinalizeEncodedBytes(state, 0)
	if err != nil {
		return err
	}

	if _, err = tx.ExecContext(ctx, "DELETE FROM semantic_coverage WHERE build_id=?", state.BuildID); err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `UPDATE semantic_build SET status='degraded',reason=?,coverage_hash=?,revision_count=0,chunk_count=0,encoded_bytes=? WHERE build_id=?`, state.SafeReason, state.CoverageHash, state.EncodedBytes, state.BuildID)

	return err
}

// Canonical forgetting retires a proven matching active build atomically.
// Pending builds retain the scrubber's targeted, incomplete revocation path.
func degradeActiveSemanticBuild(ctx context.Context, tx *sql.Tx, build string) (bool, error) {
	var active bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM semantic_active WHERE build_id=?)", build).Scan(&active); err != nil {
		return false, err
	}

	if !active {
		return false, nil
	}

	state, _, err := readSemanticState(ctx, tx, build)
	if err != nil {
		return false, err
	}

	var epochBytes []byte
	if err = tx.QueryRowContext(ctx, "SELECT epoch FROM semantic_active WHERE build_id=?", build).Scan(&epochBytes); err != nil {
		return false, err
	}

	epoch, err := decodeUint64(epochBytes)
	if err != nil {
		return false, semantic.ErrCorrupt
	}

	state, err = validateSemanticRetirement(ctx, tx, state.Scope, build, epoch)
	if err != nil {
		return false, err
	}

	return true, degradeSemanticState(ctx, tx, state)
}

func validSemanticExpectation(build string, epoch uint64) bool {
	return (build == "" && epoch == 0) || (semantic.ValidProjectionIdentity(build) && epoch != 0)
}

// The pointer remains the epoch authority even after a view is invalidated.
//
//nolint:cyclop // Absent, malformed, conflicting and exhausted pointers have distinct outcomes.
func semanticPublicationEpoch(ctx context.Context, tx *sql.Tx, scope memory.Scope, expected string, expectedEpoch uint64) (string, uint64, error) {
	var (
		build   string
		encoded []byte
	)

	err := tx.QueryRowContext(ctx, `SELECT coalesce(CASE WHEN length(CAST(build_id AS BLOB))<=512 THEN build_id END,''),epoch FROM semantic_active WHERE scope_kind=? AND scope_key=?`, string(scope.Kind), scope.Key).Scan(&build, &encoded)
	if errors.Is(err, sql.ErrNoRows) {
		if expected != "" || expectedEpoch != 0 {
			return "", 0, semantic.ErrConflict
		}

		return "", 1, nil
	}

	if err != nil {
		return "", 0, err
	}

	epoch, err := decodeUint64(encoded)
	if err != nil || !semantic.ValidProjectionIdentity(build) || epoch == 0 {
		return "", 0, semantic.ErrCorrupt
	}

	if build != expected || epoch != expectedEpoch {
		return "", 0, semantic.ErrConflict
	}

	if epoch == ^uint64(0) {
		return "", 0, semantic.ErrCapacity
	}

	if _, err = validateSemanticRetirement(ctx, tx, scope, build, epoch); err != nil {
		return "", 0, err
	}

	return build, epoch + 1, nil
}

func recheckSemanticSource(ctx context.Context, tx *sql.Tx, state semantic.State) error {
	physical, err := semanticPhysicalSource(ctx, tx, state)
	if err != nil {
		return err
	}

	var pinned string
	if err = tx.QueryRowContext(ctx, "SELECT physical_build_id FROM semantic_build WHERE build_id=?", state.BuildID).Scan(&pinned); err != nil {
		return err
	}

	if physical != pinned {
		return semantic.ErrConflict
	}

	return nil
}

func switchSemanticPublication(ctx context.Context, tx *sql.Tx, state semantic.State, previous string) error {
	const pointer = `INSERT INTO semantic_active(scope_kind,scope_key,build_id,epoch) VALUES(?,?,?,?)
 ON CONFLICT(scope_kind,scope_key) DO UPDATE SET build_id=excluded.build_id,epoch=excluded.epoch`
	if _, err := tx.ExecContext(ctx, pointer, string(state.Scope.Kind), state.Scope.Key, state.BuildID, encodeUint64(state.PublicationEpoch)); err != nil {
		return err
	}

	if previous != "" {
		// The admitted predecessor has <=8192 chunks. FK membership/coverage work
		// may exceed the staging input target; this retirement is deliberately atomic.
		if _, err := tx.ExecContext(ctx, "DELETE FROM semantic_build WHERE build_id=?", previous); err != nil {
			return err
		}
	}

	return ctx.Err()
}
