package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"sync"

	"github.com/baldaworks/boyan/pkg/memory"
)

const lexicalPageRecords = 128
const lexicalPageBytes = 2 * 1024 * 1024

// SQLiteCanonicalApplier writes disposable, unreachable physical builds.
// Physical manifest sealing and publication are deliberately separate operations.
type SQLiteCanonicalApplier struct {
	store        *Store
	reader       memory.RecallCanonicalReader
	privacy      canonicalPrivacyReader
	mu           sync.Mutex
	stages       map[memory.Scope]*lexicalStage
	checkpoints  memory.ProjectionCheckpointStore
	retention    projectionManifestWalker
	projectionID string
}

type lexicalStage struct {
	id, generation, source string
	copiedThrough          int64
	copying                bool
	failed                 bool
	pending                []revisionIdentity
}

type revisionIdentity struct {
	item, revision string
	version        uint64
}

func NewSQLiteCanonicalApplier(store *Store, reader memory.RecallCanonicalReader,
	privacy canonicalPrivacyReader) (*SQLiteCanonicalApplier, error) {
	if store == nil || nilCanonicalDependency(reader) {
		return nil, invalidCanonicalPrivacy()
	}

	applier := &SQLiteCanonicalApplier{store: store, reader: reader, privacy: privacy,
		stages: make(map[memory.Scope]*lexicalStage)}
	if err := store.bindCanonicalPrivacy(privacy); err != nil {
		return nil, err
	}

	return applier, nil
}

func stagingConflict() error {
	return memory.RetryableError(memory.CodeConflict, "sqlite staging ownership conflicts", nil)
}

// stage must be called under the scope write lease. Store ownership prevents competing appliers
// from accidentally adopting each other's incomplete build, even for equal IDs.
//
//nolint:cyclop,funcorder,funlen // Ownership and creation failure checks stay together before their consumers.
func (a *SQLiteCanonicalApplier) stage(ctx context.Context,
	scope memory.Scope, generation string, rebuild bool) (*lexicalStage, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := scope.Validate(); err != nil {
		return nil, err
	}

	if generation == "" {
		return nil, stagingConflict()
	}

	if ctx == nil {
		return nil, errors.New("sqlite apply context is nil")
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if stage := a.stages[scope]; stage != nil {
		if stage.generation != generation {
			return nil, stagingConflict()
		}

		return stage, nil
	}

	a.store.stagingMu.Lock()
	defer a.store.stagingMu.Unlock()

	if owner := a.store.stagingOwners[scope]; owner != nil && owner != a {
		return nil, stagingConflict()
	}

	if !rebuild {
		if err := a.validateIncrementalBase(ctx, scope, generation); err != nil {
			return nil, err
		}
	}

	var identity [16]byte
	if _, err := rand.Read(identity[:]); err != nil {
		return nil, err
	}

	stage := &lexicalStage{id: "lexical-" + hex.EncodeToString(identity[:]), generation: generation}

	err := a.store.write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if !rebuild {
			var activeGeneration string

			const active = `SELECT b.physical_build_id,b.generation_id
 FROM lexical_active a JOIN lexical_build b USING(physical_build_id)
 WHERE a.scope_kind=? AND a.scope_key=?`

			err := tx.QueryRowContext(ctx, active, scope.Kind, scope.Key).Scan(&stage.source, &activeGeneration)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}

			if stage.source != "" && activeGeneration != generation {
				return stagingConflict()
			}

			stage.copying = stage.source != ""
		}

		const create = `INSERT INTO lexical_build(physical_build_id,scope_kind,scope_key,generation_id,watermark)
 VALUES(?,?,?,?,?)`

		_, err := tx.ExecContext(ctx, create, stage.id, scope.Kind, scope.Key, generation, encodeUint64(0))

		return err
	})
	if err != nil {
		return nil, err
	}

	if a.store.stagingOwners == nil {
		a.store.stagingOwners = make(map[memory.Scope]*SQLiteCanonicalApplier)
	}

	a.store.stagingOwners[scope] = a
	a.stages[scope] = stage

	return stage, nil
}

func (a *SQLiteCanonicalApplier) BeginProjectionRebuild(ctx context.Context,
	scope memory.Scope, generation string) (result error) {
	defer func() { result = lexicalStorageError(result) }()

	operation, done, err := a.acquireMaterial(ctx)
	if err != nil {
		return err
	}
	defer done()

	ctx = operation

	unlock, err := a.lockScope(ctx, scope)
	if err != nil {
		return err
	}
	defer unlock()

	stage, err := a.stage(ctx, scope, generation, true)
	if err == nil && stage.source != "" {
		return stagingConflict()
	}

	return err
}

func (a *SQLiteCanonicalApplier) ApplyProjectionRebuildBatch(ctx context.Context,
	scope memory.Scope, generation string, view memory.BoundedProjectionView) error {
	if err := view.Validate(); err != nil {
		return err
	}

	if view.State.Scope != scope {
		return memory.PermanentError(memory.CodeScopeViolation, "sqlite rebuild scope mismatch", nil)
	}

	revisions := make([]memory.MemoryRevision, 0, len(view.Active))
	for _, active := range view.Active {
		revisions = append(revisions, active.Revision)
	}

	return a.Apply(ctx, scope, generation, nil, revisions)
}

// EndProjectionRebuild is only a stream boundary, never a publication claim.
func (a *SQLiteCanonicalApplier) EndProjectionRebuild(ctx context.Context,
	scope memory.Scope, generation string) (result error) {
	defer func() { result = lexicalStorageError(result) }()

	operation, done, err := a.acquireMaterial(ctx)
	if err != nil {
		return err
	}
	defer done()

	ctx = operation

	unlock, err := a.lockScope(ctx, scope)
	if err != nil {
		return err
	}
	defer unlock()

	_, err = a.stage(ctx, scope, generation, true)

	return err
}

//nolint:cyclop,funlen // Lifecycle, scope ownership and each streamed record fail independently.
func (a *SQLiteCanonicalApplier) Apply(ctx context.Context, scope memory.Scope,
	generation string, _ []memory.ScopeChange, revisions []memory.MemoryRevision) (result error) {
	defer func() { result = lexicalStorageError(result) }()

	operation, done, err := a.acquireMaterial(ctx)
	if err != nil {
		return err
	}

	ctx = operation
	defer func() {
		if result != nil && ctx.Err() != nil {
			result = ctx.Err()
		}

		done()
	}()

	unlock, err := a.lockScope(ctx, scope)
	if err != nil {
		return err
	}
	defer unlock()

	stage, err := a.stage(ctx, scope, generation, false)
	if err != nil {
		return err
	}

	if err = a.beginBatch(ctx, stage, revisions); err != nil {
		return err
	}

	if err = a.copyActive(ctx, scope, stage); err != nil {
		return err
	}

	buffer := lexicalBuffer{applier: a, build: stage.id}

	for _, revision := range revisions {
		if err = ctx.Err(); err != nil {
			return err
		}

		input, err := a.hydrateInput(ctx, scope, revision)
		if err != nil {
			return err
		}

		if err = buffer.add(ctx, input); err != nil {
			return err
		}
	}

	if err = buffer.flush(ctx); err != nil {
		return err
	}

	stage.failed = false
	stage.pending = nil

	return nil
}

func (a *SQLiteCanonicalApplier) acquire(ctx context.Context) (context.Context, func(), error) {
	if a == nil || a.store == nil {
		return nil, nil, memory.PermanentError(memory.CodeDisabled, "sqlite applier is unavailable", nil)
	}

	return a.store.acquire(ctx)
}

func (a *SQLiteCanonicalApplier) acquireMaterial(ctx context.Context) (context.Context, func(), error) {
	if a == nil || a.store == nil {
		return nil, nil, invalidCanonicalPrivacy()
	}

	if _, err := a.store.canonicalPrivacy(); err != nil {
		return nil, nil, err
	}

	return a.acquire(ctx)
}

func (a *SQLiteCanonicalApplier) lockScope(ctx context.Context, scope memory.Scope) (func(), error) {
	return a.store.lockScope(ctx, scope)
}

func (store *Store) lockScope(ctx context.Context, scope memory.Scope) (func(), error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}

	candidate := make(chan struct{}, 1)
	candidate <- struct{}{}

	value, _ := store.scopeWrites.LoadOrStore(scope, candidate)

	lease := value.(chan struct{})
	select {
	case <-lease:
		return func() { lease <- struct{}{} }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

//nolint:cyclop // Exact canonical identity checks precede preparation of the one hydrated record.
func (a *SQLiteCanonicalApplier) hydrateInput(ctx context.Context, scope memory.Scope,
	revision memory.MemoryRevision) (lexicalInput, error) {
	if revision.RevisionID == "" {
		return lexicalInput{}, memory.PermanentError(memory.CodeInvalidDerived, "sqlite revision identity is required", nil)
	}

	records, err := a.reader.LoadRecallRecords(ctx, scope, []string{revision.RevisionID})
	if err != nil {
		return lexicalInput{}, err
	}

	if len(records) == 0 {
		return lexicalInput{revision: revision.RevisionID}, nil
	}

	if len(records) != 1 {
		return lexicalInput{}, stagingConflict()
	}

	record := records[0]
	if record.Scope != scope || record.RevisionID != revision.RevisionID ||
		(revision.ItemID != "" && revision.ItemID != record.ItemID) ||
		(revision.Revision != 0 && revision.Revision != record.Revision) {
		return lexicalInput{}, memory.PermanentError(memory.CodeScopeViolation, "sqlite canonical revision mismatch", nil)
	}

	input, err := canonicalLexicalInput(record)
	if err != nil {
		return lexicalInput{}, err
	}

	return admitLexicalInput(ctx, a.privacy, scope, input)
}

func (a *SQLiteCanonicalApplier) copyActive(ctx context.Context, scope memory.Scope, stage *lexicalStage) error {
	buffer := lexicalBuffer{applier: a, build: stage.id}
	if stage.copying {
		if err := a.validateCopyOwners(ctx, scope, stage); err != nil {
			return err
		}
	}

	after := stage.copiedThrough
	for stage.copying {
		rows, last, err := a.store.loadLexicalPage(ctx, stage.source, after)
		if err != nil {
			return err
		}

		if len(rows) == 0 {
			break
		}

		input, err := admitLexicalInput(ctx, a.privacy, scope, rows[0])
		if err != nil {
			return err
		}

		if err = buffer.add(ctx, input); err != nil {
			return err
		}

		clear(rows)

		after = last
	}

	if err := buffer.flush(ctx); err != nil {
		return err
	}

	stage.copiedThrough = after
	stage.copying = false

	return nil
}

func (a *SQLiteCanonicalApplier) writeInputs(ctx context.Context, build string, inputs []lexicalInput) error {
	return a.store.write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		for _, input := range inputs {
			if err := ctx.Err(); err != nil {
				return err
			}

			if err := input.write(ctx, tx, build); err != nil {
				return err
			}
		}

		return nil
	})
}

var _ memory.ProjectionBatchRebuildApplier = (*SQLiteCanonicalApplier)(nil)
