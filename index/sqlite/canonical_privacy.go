package sqlite

import (
	"context"
	"reflect"

	"github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

// canonicalPrivacyReader is the immutable, content-free admission authority.
// Hydration is still required, but a previously hydrated record is not admission.
type canonicalPrivacyReader interface {
	IsSourceDenied(ctx context.Context, scope memory.Scope, sourceID string) (bool, error)
	IsRevisionDenied(ctx context.Context, scope memory.Scope, revisionID string) (bool, error)
	LoadCanonicalRevisions(ctx context.Context,
		request memory.CanonicalRevisionReadRequest) ([]memory.MemoryRevision, error)
}

func invalidCanonicalPrivacy() error {
	return memory.PermanentError(memory.CodeStoreFailure, "sqlite canonical dependencies are invalid", nil)
}

// Reflection is confined to construction: interface nils and identity validity
// are checked before any store mutation, never to select a runtime backend.
func nilCanonicalDependency(value any) bool {
	if value == nil {
		return true
	}

	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func (store *Store) bindCanonicalPrivacy(privacy canonicalPrivacyReader) error {
	if store == nil || nilCanonicalDependency(privacy) || !reflect.ValueOf(privacy).Comparable() {
		return invalidCanonicalPrivacy()
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	if store.closing {
		return ErrClosed
	}

	if store.privacy != nil && store.privacy != privacy {
		return memory.PermanentError(memory.CodeConflict, "sqlite canonical authority conflicts", nil)
	}

	store.privacy = privacy

	return nil
}

func (store *Store) canonicalPrivacy() (canonicalPrivacyReader, error) {
	if store == nil {
		return nil, invalidCanonicalPrivacy()
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	if store.closing {
		return nil, ErrClosed
	}

	if store.privacy == nil {
		return nil, invalidCanonicalPrivacy()
	}

	return store.privacy, nil
}

type privacyDenial struct {
	revision bool
	sourceID string
}

func (denial privacyDenial) denied() bool { return denial.revision || denial.sourceID != "" }

// A negative answer lasts only for the caller's current scope admission. Load
// one scoped structural revision at a time; its immutable Evidence survives erase.
func deniedCanonicalRevision(ctx context.Context, privacy canonicalPrivacyReader,
	scope memory.Scope, revisionID string) (privacyDenial, error) {
	if err := ctx.Err(); err != nil {
		return privacyDenial{}, err
	}

	denied, err := privacy.IsRevisionDenied(ctx, scope, revisionID)
	if ctx.Err() != nil {
		return privacyDenial{}, ctx.Err()
	}

	if err != nil || denied {
		return privacyDenial{revision: denied}, err
	}

	revisions, err := privacy.LoadCanonicalRevisions(ctx, memory.CanonicalRevisionReadRequest{
		Scope: scope, RevisionIDs: []string{revisionID},
	})
	if ctx.Err() != nil {
		return privacyDenial{}, ctx.Err()
	}

	if err != nil {
		return privacyDenial{}, err
	}

	if len(revisions) != 1 || revisions[0].RevisionID != revisionID {
		return privacyDenial{}, invalidCanonicalPrivacy()
	}

	return deniedCanonicalEvidence(ctx, privacy, scope, revisions[0].Evidence)
}

func deniedCanonicalEvidence(ctx context.Context, privacy canonicalPrivacyReader,
	scope memory.Scope, references []memory.EvidenceRef) (privacyDenial, error) {
	// Avoid a second evidence-sized cache. Repeated source IDs are compared
	// against the immutable input, keeping only one structural record alive.
	for i, evidence := range references {
		if evidence.SourceID == "" {
			return privacyDenial{}, invalidCanonicalPrivacy()
		}

		duplicate := false

		for _, prior := range references[:i] {
			if prior.SourceID == evidence.SourceID {
				duplicate = true

				break
			}
		}

		if duplicate {
			continue
		}

		denied, err := deniedCanonicalSource(ctx, privacy, scope, evidence.SourceID)
		if err != nil || denied {
			return privacyDenial{sourceID: evidence.SourceID}, err
		}
	}

	return privacyDenial{}, nil
}

func deniedCanonicalSource(ctx context.Context, privacy canonicalPrivacyReader,
	scope memory.Scope, sourceID string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	denied, err := privacy.IsSourceDenied(ctx, scope, sourceID)
	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	return denied, err
}

func admitLexicalInput(ctx context.Context, privacy canonicalPrivacyReader,
	scope memory.Scope, input lexicalInput) (lexicalInput, error) {
	if len(input.values) == 0 {
		return input, nil
	}

	denial, err := deniedCanonicalRevision(ctx, privacy, scope, input.revision)
	if err != nil {
		return lexicalInput{}, err
	}

	if denial.denied() {
		return lexicalInput{revision: input.revision}, nil
	}

	for _, sourceID := range input.sources {
		denied, err := deniedCanonicalSource(ctx, privacy, scope, sourceID)
		if err != nil {
			return lexicalInput{}, err
		}

		if denied {
			return lexicalInput{revision: input.revision}, nil
		}
	}

	return input, nil
}

func admitSemanticPopulation(ctx context.Context, privacy canonicalPrivacyReader, scope memory.Scope,
	coverage []semantic.CoverageRecord, chunks []semantic.ProjectionChunk) error {
	for _, record := range coverage {
		denial, err := deniedCanonicalRevision(ctx, privacy, scope, record.RevisionID)
		if err != nil {
			return err
		}

		if denial.denied() {
			return semantic.ErrConflict
		}
	}

	for _, chunk := range chunks {
		denial, err := deniedCanonicalRevision(ctx, privacy, scope, chunk.RevisionID)
		if err != nil {
			return err
		}

		if denial.denied() {
			return semantic.ErrConflict
		}

		for _, sourceID := range chunk.SourceIDs {
			denied, err := deniedCanonicalSource(ctx, privacy, scope, sourceID)
			if err != nil {
				return err
			}

			if denied {
				return semantic.ErrConflict
			}
		}
	}

	return ctx.Err()
}

// The scope lease excludes production revocation during this closed check and
// copy. Neither a revoked source nor destination can be reused by lazy commit.
func (a *SQLiteCanonicalApplier) validateCopyOwners(ctx context.Context,
	scope memory.Scope, stage *lexicalStage) error {
	var valid bool

	err := a.store.reader.QueryRowContext(ctx, `SELECT EXISTS(
 SELECT 1 FROM lexical_build source JOIN lexical_build destination
 ON destination.physical_build_id=?
 WHERE source.physical_build_id=? AND source.scope_kind=? AND source.scope_key=?
 AND source.generation_id=? AND source.state='committed' AND source.revoked=0
 AND destination.scope_kind=source.scope_kind AND destination.scope_key=source.scope_key
 AND destination.generation_id=source.generation_id AND destination.revoked=0)`,
		stage.id, stage.source, scope.Kind, scope.Key, stage.generation).Scan(&valid)
	if err != nil {
		return err
	}

	if !valid {
		return stagingConflict()
	}

	return nil
}
