package sqlite

import (
	"context"
	"time"

	"github.com/baldaworks/boyan/pkg/memory"
)

const certifiedChannelDepth = 100
const recallCandidateMultiplier = 4

// SearchRecallCertified binds lexical identities to the complete build pinned
// by the SQL snapshot and rechecks both stores after the query. Canonical
// hydration remains necessary before any remembered text is released.
func (store *Store) SearchRecallCertified(ctx context.Context, request memory.RecallRequest,
	depth int) (result memory.CertifiedRecallResult, resultErr error) {
	return store.searchRecallCertified(ctx, request, depth, certifiedChannelDepth)
}

//nolint:cyclop // The certificate checks, query and postchecks must propagate every independent failure.
func (store *Store) searchRecallCertified(ctx context.Context, request memory.RecallRequest,
	depth, maximum int) (result memory.CertifiedRecallResult, resultErr error) {
	defer func() { resultErr = lexicalStorageError(resultErr) }()

	if store == nil {
		return result, memory.PermanentError(memory.CodeDisabled, "sqlite projection is unavailable", nil)
	}

	effective := time.Now().UTC()
	if request.AsOf != nil {
		effective = request.AsOf.UTC()
	}

	normalized, err := memory.NormalizeRecallRequest(request)
	if err != nil {
		return result, err
	}

	if depth < 1 || depth > maximum {
		return result, memory.PermanentError(memory.CodeInvalidQuery, "invalid recall channel depth", nil)
	}

	operation, done, err := store.acquire(ctx)
	if err != nil {
		return result, err
	}
	defer done()

	ctx = operation

	before, canonical, err := store.canonicalManifest(ctx, normalized.Scope)
	if err != nil {
		return result, err
	}

	result, physical, err := store.searchCertifiedSnapshot(ctx, normalized, effective, depth, before, canonical)
	if err != nil {
		return result, err
	}

	if result.Certified {
		result.Manifest = before

		result.Certified, err = store.stillCertified(ctx, physical, before)
		if err != nil {
			return memory.CertifiedRecallResult{}, err
		}
	}

	if normalized.MinScopeChangeSeq != 0 && !result.Certified {
		return memory.CertifiedRecallResult{}, stagingConflict()
	}

	return result, nil
}

//nolint:cyclop // Require every independent physical, minimum and query gate inside the pinned snapshot.
func (store *Store) searchCertifiedSnapshot(ctx context.Context, normalized memory.RecallRequest,
	effective time.Time, depth int, before memory.ProjectionManifest, canonical bool) (
	result memory.CertifiedRecallResult, physical physicalLexicalManifest, resultErr error) {
	snapshot, err := store.read(ctx)
	if err != nil {
		return result, physical, err
	}

	defer func() { _ = snapshot.Close() }()

	build, err := snapshot.activeLexicalBuild(normalized.Scope)
	if err != nil {
		return result, physical, err
	}

	var complete bool
	if canonical {
		physical, complete, err = snapshot.committedLexical(normalized.Scope, before.GenerationID, before.Watermark, true)
		if err != nil {
			return result, physical, err
		}
	}

	result.Certified = canonical && complete && physical.id == build
	if normalized.MinScopeChangeSeq != 0 && (!result.Certified || before.Watermark < normalized.MinScopeChangeSeq) {
		return result, physical, stagingConflict()
	}

	result.Hits, err = snapshot.searchLexical(normalized, effective, build, depth)
	if err != nil {
		return result, physical, err
	}

	if err := snapshot.Close(); err != nil {
		return result, physical, err
	}

	return result, physical, nil
}

func (store *Store) stillCertified(ctx context.Context, physical physicalLexicalManifest,
	before memory.ProjectionManifest) (bool, error) {
	after, found, err := store.canonicalManifest(ctx, before.Scope)
	if err != nil || !found || !sameProjectionTuple(before, after) {
		return false, err
	}

	snapshot, err := store.read(ctx)
	if err != nil {
		return false, err
	}

	defer func() { _ = snapshot.Close() }()

	current, found, err := snapshot.committedLexical(before.Scope, before.GenerationID, before.Watermark, true)
	if err != nil || !found || current.id != physical.id {
		return false, err
	}

	last, found, err := store.canonicalManifest(ctx, before.Scope)

	return found && sameProjectionTuple(before, last), err
}

// SearchRecallConsistent requires an unchanged complete certificate even for
// empty results and requests whose minimum is zero.
func (store *Store) SearchRecallConsistent(ctx context.Context,
	request memory.RecallRequest) ([]memory.RecallProjectionHit, error) {
	normalized, err := memory.NormalizeRecallRequest(request)
	if err != nil {
		return nil, err
	}

	depth := min(normalized.Limit*recallCandidateMultiplier, memory.MaxRecallCandidates)

	result, err := store.searchRecallCertified(ctx, request, depth, memory.MaxRecallCandidates)
	if err != nil {
		return nil, err
	}

	if !result.Certified {
		return nil, stagingConflict()
	}

	return result.Hits, nil
}

var _ memory.CertifiedRecallProjection = (*Store)(nil)
var _ memory.ConsistentRecallProjection = (*Store)(nil)
