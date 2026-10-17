//nolint:funcorder // Keep snapshot opening and its pinning gates together before the channel methods.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	semantic "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

// sqlSearchSnapshot owns one MVCC view; metadata never changes after opening.
type sqlSearchSnapshot struct {
	*readSnapshot

	scope     memory.Scope
	effective time.Time
	lexical   physicalLexicalManifest
	state     semantic.State
	found     bool
	stateErr  error
}

// OpenSearchSnapshot pins both active pointers before returning. A metadata-only
// preflight may precede inference if it closes completely first. The shared
// channel snapshot opens after query vectors are ready.
func (store *Store) OpenSearchSnapshot(ctx context.Context, scope memory.Scope) (semantic.SearchSnapshot, error) {
	return store.openSearchSnapshot(ctx, scope, time.Now)
}

func (store *Store) openSearchSnapshot(ctx context.Context, scope memory.Scope,
	now func() time.Time) (semantic.SearchSnapshot, error) {
	effective := now().UTC()

	if ctx == nil {
		return nil, semantic.ErrInvalid
	}

	if err := scope.Validate(); err != nil {
		return nil, err
	}

	if store == nil {
		return nil, semantic.ErrClosed
	}

	read, err := store.read(ctx)
	if err != nil {
		return nil, semanticStorageError(err)
	}

	snapshot := &sqlSearchSnapshot{readSnapshot: read, scope: scope, effective: effective}
	if err = snapshot.pinLexical(); err != nil {
		_ = snapshot.Close()

		return nil, lexicalStorageError(err)
	}

	if store.embeddingsEnabled {
		snapshot.state, snapshot.found, snapshot.stateErr = activeSemanticState(read.ctx, read.tx, scope)
		if snapshot.stateErr == nil && snapshot.found {
			snapshot.stateErr = snapshot.checkSemanticSource()
		}
	}

	if err = read.ctx.Err(); err != nil {
		_ = snapshot.Close()

		return nil, err
	}

	return snapshot, nil
}

func (snapshot *sqlSearchSnapshot) pinLexical() error {
	var (
		generation string
		watermark  []byte
	)

	err := snapshot.tx.QueryRowContext(snapshot.ctx, `SELECT b.generation_id,
 CASE WHEN typeof(b.watermark)='blob' AND length(b.watermark)=8 THEN b.watermark END
 FROM lexical_active a JOIN lexical_build b USING(physical_build_id,scope_kind,scope_key)
 WHERE a.scope_kind=? AND a.scope_key=?`, snapshot.scope.Kind, snapshot.scope.Key).Scan(&generation, &watermark)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}

	if err != nil {
		return err
	}

	sequence, err := decodeUint64(watermark)
	if err != nil {
		return err
	}

	physical, found, err := snapshot.committedLexical(snapshot.scope, generation, sequence, true)
	if err != nil {
		return err
	}

	if found {
		snapshot.lexical = physical
	}

	return nil
}

func (snapshot *sqlSearchSnapshot) checkSemanticSource() error {
	state := snapshot.state

	manifest := snapshot.lexical.manifest
	if manifest.ProjectionID == "" || state.Scope != manifest.Scope ||
		state.LexicalGenerationID != manifest.GenerationID || state.LexicalWatermark != manifest.Watermark {
		return semantic.ErrConflict
	}

	var matches bool

	err := snapshot.tx.QueryRowContext(snapshot.ctx, `SELECT physical_build_id=? FROM semantic_build WHERE build_id=?`,
		snapshot.lexical.id, state.BuildID).Scan(&matches)
	if err != nil {
		return err
	}

	if !matches {
		return semantic.ErrConflict
	}

	return nil
}

func (snapshot *sqlSearchSnapshot) LexicalManifest() memory.ProjectionManifest {
	return snapshot.lexical.manifest
}

func (snapshot *sqlSearchSnapshot) SemanticState() (semantic.State, bool, error) {
	return snapshot.state, snapshot.found, semanticStorageError(snapshot.stateErr)
}

// requestContext keeps call cancellation separate from snapshot ownership. A
// canceled call interrupts its SQL work; the opening context owns the snapshot.
func (snapshot *sqlSearchSnapshot) requestContext(ctx context.Context) (context.Context, func(), error) {
	if ctx == nil {
		return nil, nil, semantic.ErrInvalid
	}

	if err := snapshot.ctx.Err(); err != nil {
		return nil, nil, err
	}

	operation, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(snapshot.ctx, cancel)

	return operation, func() { stop(); cancel() }, nil
}

func (snapshot *sqlSearchSnapshot) normalize(request memory.RecallRequest) (memory.RecallRequest, time.Time, error) {
	normalized, err := memory.NormalizeRecallRequest(request)
	if err != nil {
		return normalized, time.Time{}, err
	}

	if normalized.Scope != snapshot.scope {
		return normalized, time.Time{}, semantic.ErrInvalid
	}

	effective := snapshot.effective
	if normalized.AsOf != nil {
		effective = normalized.AsOf.UTC()
	}

	return normalized, effective, nil
}

func (snapshot *sqlSearchSnapshot) SearchLexical(ctx context.Context, request memory.RecallRequest,
	limit int) (hits []memory.RecallProjectionHit, result error) {
	operation, done, err := snapshot.requestContext(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	defer func() {
		if result != nil && operation.Err() != nil {
			result = operation.Err()
		}

		result = semanticStorageError(result)
	}()

	normalized, effective, err := snapshot.normalize(request)
	if err != nil {
		return nil, err
	}

	read := readSnapshot{tx: snapshot.tx, ctx: operation}

	return read.searchLexicalChannel(normalized, effective, snapshot.lexical.id, limit)
}

var _ semantic.SearchSnapshot = (*sqlSearchSnapshot)(nil)
