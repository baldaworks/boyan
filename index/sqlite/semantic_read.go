package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"math"
	"time"

	semantic "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

func semanticOptionalTime(value *time.Time) []byte {
	if value == nil {
		return nil
	}

	return encodeTime(*value)
}

func semanticPhysicalSource(ctx context.Context, tx *sql.Tx, state semantic.State) (string, error) {
	snapshot := readSnapshot{tx: tx, ctx: ctx}

	physical, found, err := snapshot.committedLexical(state.Scope, state.LexicalGenerationID,
		state.LexicalWatermark, true)
	if err != nil {
		return "", err
	}

	if !found {
		return "", semantic.ErrConflict
	}

	return physical.id, nil
}

// ActiveState reads bounded control metadata only. Incomplete and corrupt
// active builds fail closed; legitimate unreachable staging builds are ignored.
func (store *Store) ActiveState(ctx context.Context, scope memory.Scope) (semantic.State, bool, error) {
	if ctx == nil || semantic.ValidateScope(scope) != nil {
		return semantic.State{}, false, semantic.ErrInvalid
	}

	if !store.embeddingsEnabled {
		return semantic.State{}, false, nil
	}

	snapshot, err := store.read(ctx)
	if err != nil {
		return semantic.State{}, false, semanticStorageError(err)
	}

	defer func() { _ = snapshot.Close() }()

	state, found, err := activeSemanticState(snapshot.ctx, snapshot.tx, scope)

	return state, found, semanticStorageError(err)
}

//nolint:cyclop // Pointer bounds, scope and complete epoch identity are independent integrity gates.
func activeSemanticState(ctx context.Context, tx *sql.Tx, scope memory.Scope) (semantic.State, bool, error) {
	var (
		build sql.NullString
		epoch []byte
	)

	const query = `SELECT CASE WHEN length(CAST(build_id AS BLOB))<=512 THEN build_id END,epoch
  FROM semantic_active WHERE scope_kind=? AND scope_key=?`

	err := tx.QueryRowContext(ctx, query, string(scope.Kind), scope.Key).Scan(&build, &epoch)
	if errors.Is(err, sql.ErrNoRows) {
		return semantic.State{}, false, nil
	}

	if err != nil {
		return semantic.State{}, false, err
	}

	if !build.Valid || !semantic.ValidProjectionIdentity(build.String) {
		return semantic.State{}, false, semantic.ErrCorrupt
	}

	state, complete, err := readSemanticState(ctx, tx, build.String)
	if err != nil {
		return semantic.State{}, false, err
	}

	activeEpoch, err := decodeUint64(epoch)
	if err != nil || !complete || state.Scope != scope || state.PublicationEpoch != activeEpoch || activeEpoch == 0 {
		return semantic.State{}, false, semantic.ErrCorrupt
	}

	return state, true, nil
}

// LoadReusable is the build-only vector copy path. Recall must use SQL ranking.
func (store *Store) LoadReusable(ctx context.Context, view semantic.View) (semantic.Reusable, bool, error) {
	if ctx == nil || semantic.ValidateView(view) != nil {
		return semantic.Reusable{}, false, semantic.ErrInvalid
	}

	if !store.embeddingsEnabled {
		return semantic.Reusable{}, false, nil
	}

	snapshot, err := store.read(ctx)
	if err != nil {
		return semantic.Reusable{}, false, semanticStorageError(err)
	}

	defer func() { _ = snapshot.Close() }()

	state, found, err := activeSemanticState(snapshot.ctx, snapshot.tx, view.Scope)
	if err != nil || !found {
		return semantic.Reusable{}, false, semanticStorageError(err)
	}

	if state.Status != semantic.StatusReady || !semantic.StateMatchesView(state, view) {
		return semantic.Reusable{}, false, nil
	}

	result := semantic.Reusable{State: state}

	_, err = validateSemanticBuild(snapshot.ctx, snapshot.tx, state.BuildID, func(chunk semantic.ProjectionChunk) {
		result.Chunks = append(result.Chunks, semantic.ReusableChunk{
			ItemID:      chunk.ItemID,
			RevisionID:  chunk.RevisionID,
			Revision:    chunk.Revision,
			ContentHash: chunk.ContentHash,
			InputHash:   chunk.InputHash,
			Ordinal:     chunk.Ordinal,
			Vector:      chunk.Vector,
		})
	})
	if err != nil {
		return semantic.Reusable{}, false, semanticStorageError(err)
	}

	return result, true, nil
}

// ObservedMetadataReady avoids decoding or loading vectors on status requests.
//
//nolint:cyclop // Keep tuple, count and timestamp rejection gates explicit.
func (store *Store) ObservedMetadataReady(ctx context.Context, view semantic.View) (bool, error) {
	if ctx == nil || semantic.ValidateView(view) != nil {
		return false, semantic.ErrInvalid
	}

	if !store.embeddingsEnabled {
		return false, nil
	}

	snapshot, err := store.read(ctx)
	if err != nil {
		return false, semanticStorageError(err)
	}

	defer func() { _ = snapshot.Close() }()

	state, found, err := activeSemanticState(snapshot.ctx, snapshot.tx, view.Scope)
	if err != nil || !found {
		return false, semanticStorageError(err)
	}

	if state.Status != semantic.StatusReady || !semantic.StateMatchesView(state, view) {
		return false, nil
	}

	rows, err := snapshot.tx.QueryContext(
		snapshot.ctx,
		"SELECT observed_at FROM semantic_chunk WHERE build_id=? LIMIT 8193",
		state.BuildID,
	)
	if err != nil {
		return false, semanticStorageError(err)
	}

	defer func() { _ = rows.Close() }()

	count := uint32(0)

	for rows.Next() {
		var encoded []byte
		if err = rows.Scan(&encoded); err != nil {
			return false, semanticStorageError(err)
		}

		observed, decodeErr := decodeTime(encoded)
		if decodeErr != nil || observed.IsZero() || count >= state.ChunkCount {
			return false, semantic.ErrCorrupt
		}

		count++
	}

	if err = rows.Err(); err != nil {
		return false, semanticStorageError(err)
	}

	if count != state.ChunkCount {
		return false, semantic.ErrCorrupt
	}

	return true, nil
}

// readSemanticState bounds variable-size values before materializing them.
//
//nolint:cyclop,funlen // Stored metadata is untrusted; retain each independent validation gate.
func readSemanticState(ctx context.Context, tx *sql.Tx, build string) (semantic.State, bool, error) {
	state := semantic.State{SchemaVersion: semantic.ProjectionSchemaVersion, BuildID: build}

	var (
		watermark, epoch, ready []byte
		status                  string
		complete                bool
	)

	const query = `SELECT scope_kind,scope_key,generation_id,watermark,model_space,dimensions,epoch,complete,
  status,reason,coverage_hash,revision_count,chunk_count,encoded_bytes,omitted_ineligible,omitted_expired,
  omitted_sensitive,ready_at FROM semantic_build WHERE build_id=?
  AND length(CAST(scope_kind AS BLOB))<=512 AND length(CAST(scope_key AS BLOB))<=4096
  AND length(CAST(generation_id AS BLOB))<=512 AND length(CAST(model_space AS BLOB))<=64
  AND length(CAST(reason AS BLOB))<=512 AND length(CAST(coverage_hash AS BLOB))<=64
  AND omitted_ineligible<=4294967295 AND omitted_expired<=4294967295 AND omitted_sensitive<=4294967295`

	err := tx.QueryRowContext(ctx, query, build).
		Scan(&state.Scope.Kind, &state.Scope.Key, &state.LexicalGenerationID, &watermark,
			&state.ModelSpace, &state.Dimensions, &epoch, &complete, &status, &state.SafeReason, &state.CoverageHash,
			&state.RecordCount, &state.ChunkCount, &state.EncodedBytes, &state.Omitted.Ineligible,
			&state.Omitted.Expired, &state.Omitted.Sensitive, &ready)
	if errors.Is(err, sql.ErrNoRows) {
		return semantic.State{}, false, semantic.ErrCorrupt
	}

	if err != nil {
		return semantic.State{}, false, err
	}

	state.Status = semantic.Status(status)
	if complete && status == "building" {
		return semantic.State{}, false, semantic.ErrCorrupt
	}

	if status == "building" {
		state.Status = semantic.StatusReady
	}

	state.LexicalWatermark, err = decodeUint64(watermark)
	if err != nil {
		return semantic.State{}, false, semantic.ErrCorrupt
	}

	state.PublicationEpoch, err = decodeUint64(epoch)
	if err != nil {
		return semantic.State{}, false, semantic.ErrCorrupt
	}

	state.ReadyAt, err = decodeTime(ready)
	if err != nil || semantic.ValidateGeneration(semantic.StateFromGeneration(state)) != nil ||
		semantic.CheckProjectionBounds(
			int(state.ChunkCount),
			state.EncodedBytes,
		) != nil || state.RecordCount > semantic.ProjectionChunkLimit ||
		(state.Status != semantic.StatusReady && state.Status != semantic.StatusDegraded) {
		return semantic.State{}, false, semantic.ErrCorrupt
	}

	if state.Status == semantic.StatusReady && (!semantic.ValidHash(state.CoverageHash) || state.SafeReason != "") {
		return semantic.State{}, false, semantic.ErrCorrupt
	}

	return state, complete, nil
}

func decodeSemanticVector(encoded []byte, dimensions int) ([]float32, error) {
	if dimensions < 1 || dimensions > semantic.MaxEmbeddingDimensions ||
		len(encoded) != dimensions*semantic.Float32Bytes {
		return nil, semantic.ErrCorrupt
	}

	result := make([]float32, dimensions)
	for index := range result {
		result[index] = math.Float32frombits(binary.LittleEndian.Uint32(encoded[index*semantic.Float32Bytes:]))
	}

	if semantic.ValidateVector(result, dimensions) != nil {
		return nil, semantic.ErrCorrupt
	}

	return result, nil
}
