package semanticprojection

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/baldaworks/boyan/internal/embedding"
	semantic "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

// ScopeStateReader supplies canonical sequence authority without mutation access.
type ScopeStateReader interface {
	LoadScopeState(ctx context.Context, scope memory.Scope) (memory.ScopeState, error)
}

// HybridDependencies binds the SQL snapshot reader and its canonical authority.
// Composition supplies the lexical fallback explicitly; there is no backend selection.
type HybridDependencies struct {
	Canonical  ScopeStateReader
	Manifests  memory.RecallProjectionCheckpointReader
	Reader     SearchReader
	Lexical    memory.RecallProjection
	Provider   embedding.Provider
	ModelSpace semantic.ModelSpace
}

// Hybrid returns certified SQL identities only. Canonical hydration remains the
// caller's privacy barrier before releasing any remembered text.
type Hybrid struct {
	canonical ScopeStateReader
	manifests memory.RecallProjectionCheckpointReader
	reader    SearchReader
	lexical   memory.RecallProjection
	provider  embedding.Provider
	space     semantic.ModelSpace
	spaceID   string
	now       func() time.Time
}

// NewHybrid constructs the shared-snapshot SQLite orchestration.
func NewHybrid(deps HybridDependencies) (*Hybrid, error) {
	spaceID, err := semantic.SpaceFingerprint(deps.ModelSpace)
	if err != nil {
		return nil, err
	}

	for _, dependency := range []any{deps.Canonical, deps.Manifests, deps.Reader, deps.Lexical, deps.Provider} {
		if nilDependency(dependency) {
			return nil, fmt.Errorf("hybrid dependencies are required: %w", semantic.ErrInvalid)
		}
	}

	return &Hybrid{canonical: deps.Canonical, manifests: deps.Manifests, reader: deps.Reader,
		lexical: deps.Lexical, provider: deps.Provider, space: deps.ModelSpace, spaceID: spaceID, now: time.Now}, nil
}

func (hybrid *Hybrid) SearchRecall(ctx context.Context,
	request memory.RecallRequest) ([]memory.RecallProjectionHit, error) {
	hits, _, err := hybrid.SearchRecallWithReport(ctx, request)

	return hits, err
}

func (hybrid *Hybrid) SearchRecallConsistent(ctx context.Context,
	request memory.RecallRequest) ([]memory.RecallProjectionHit, error) {
	return hybrid.SearchRecall(ctx, request)
}

func (hybrid *Hybrid) SearchRecallWithReport(ctx context.Context,
	request memory.RecallRequest) ([]memory.RecallProjectionHit, *memory.RetrievalReport, error) {
	// Capture before normalization, provider work, or waiting for a SQL connection.
	at := hybrid.now().UTC()
	if request.AsOf == nil {
		request.AsOf = &at
	}

	request, err := normalizeHybridRequest(ctx, request)
	if err != nil {
		return nil, nil, err
	}

	report := &memory.RetrievalReport{Requested: "hybrid", Effective: "hybrid", ModelSpacePrefix: hybrid.spaceID[:12]}

	hits, err := hybrid.search(ctx, request, report)
	if err != nil {
		return nil, nil, hybridReadError(err)
	}

	return hits, report, nil
}

//nolint:cyclop,funlen // Keep the ordered availability/provider/channel failure boundaries explicit.
func (hybrid *Hybrid) search(ctx context.Context, request memory.RecallRequest,
	report *memory.RetrievalReport) ([]memory.RecallProjectionHit, error) {
	prepared, err := semantic.PrepareQuery(ctx, request.Query, hybrid.space.QueryPrefix)
	if err != nil {
		return nil, err
	}

	report.OmittedQueryRunes, report.OmittedQueryWindows = prepared.OmittedRunes, prepared.OmittedWindows

	availability, err := hybrid.probeAvailability(ctx, request.Scope)
	if err != nil {
		return nil, err
	}

	source, certified, err := hybrid.certifiedSource(ctx, request)
	if err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if !certified || !availability.found || !sameHybridTuple(availability.manifest, source) ||
		!hybrid.matchesState(availability.state, source) {
		return hybrid.fallback(ctx, request, report, "unavailable")
	}

	vectors, providerErr := embedHybridQuery(ctx, hybrid.provider, prepared, hybrid.space.Dimensions)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if errors.Is(providerErr, context.Canceled) || errors.Is(providerErr, context.DeadlineExceeded) {
		return nil, providerErr
	}

	before, certified, err := hybrid.certifiedSource(ctx, request)
	if err != nil {
		return nil, err
	}

	if providerErr != nil {
		return hybrid.fallback(ctx, request, report, safeRecallReason(providerErr))
	}

	if !certified {
		return hybrid.fallback(ctx, request, report, "unavailable")
	}

	result, err := hybrid.readChannels(ctx, request, before, vectors)
	if err != nil {
		return nil, err
	}

	if !result.certified {
		return hybrid.fallback(ctx, request, report, "unavailable")
	}

	unchanged, err := hybrid.recertify(ctx, request, before, result.state)
	if err != nil {
		return nil, err
	}

	if !unchanged {
		return hybrid.fallback(ctx, request, report, "unavailable")
	}

	report.LexicalCandidates = len(result.lexical)
	report.SemanticCandidates = len(result.dense)
	report.ScannedChunks = result.counts.ScannedChunks

	return fuseHybridMatches(ctx, request.Scope, result.lexical, result.dense)
}

func embedHybridQuery(ctx context.Context, provider embedding.Provider,
	prepared semantic.PreparedText, dimensions int) ([][]float32, error) {
	inputs := make([]string, len(prepared.Windows))
	for i, window := range prepared.Windows {
		inputs[i] = window.Input
	}

	vectors, err := provider.Embed(ctx, inputs)
	if err != nil {
		return nil, err
	}

	if len(vectors) != len(inputs) {
		return nil, &embedding.Error{Code: embedding.CodeInvalidResponse}
	}

	for _, vector := range vectors {
		if err := semantic.ValidateVector(vector, dimensions); err != nil {
			return nil, err
		}
	}

	return vectors, nil
}

//nolint:cyclop // All independent scope, sequence and active-certificate gates must hold.
func (hybrid *Hybrid) certifiedSource(ctx context.Context, request memory.RecallRequest) (
	memory.ProjectionManifest, bool, error) {
	state, err := hybrid.canonical.LoadScopeState(ctx, request.Scope)
	if err != nil {
		return memory.ProjectionManifest{}, false, err
	}

	manifest, found, err := hybrid.manifests.LoadActiveProjectionManifest(ctx, request.Scope, "sqlite")
	if err != nil {
		return memory.ProjectionManifest{}, false, err
	}

	physical, active, err := hybrid.reader.LoadActivePhysicalProjectionManifest(ctx, request.Scope, "sqlite")
	if err != nil {
		return memory.ProjectionManifest{}, false, err
	}

	valid := found && active && manifest == physical && manifest.Scope == request.Scope &&
		manifest.ProjectionID == "sqlite" && manifest.Status == memory.ProjectionGenerationActive &&
		manifest.Validate() == nil && state.Scope == request.Scope && state.Validate() == nil &&
		state.ChangeSeq == manifest.Watermark && manifest.Watermark >= request.MinScopeChangeSeq

	return manifest, valid, nil
}

type hybridChannels struct {
	lexical   []memory.RecallProjectionHit
	dense     []semantic.DenseMatch
	counts    semantic.ScanCounts
	state     semantic.State
	certified bool
}

// readChannels closes the shared transaction before any recertification/fallback.
//
//nolint:cyclop // Lexical and semantic failures have intentionally different policies.
func (hybrid *Hybrid) readChannels(ctx context.Context, request memory.RecallRequest,
	manifest memory.ProjectionManifest, vectors [][]float32) (result hybridChannels, err error) {
	snapshot, err := hybrid.reader.OpenSearchSnapshot(ctx, request.Scope)
	if err != nil {
		return result, err
	}
	defer func() {
		if closeErr := snapshot.Close(); err == nil {
			err = closeErr
		}
	}()

	if !sameHybridTuple(snapshot.LexicalManifest(), manifest) {
		return result, nil
	}

	depth := semantic.CandidateLimit(request.Limit)

	result.lexical, err = snapshot.SearchLexical(ctx, request, depth)
	if err != nil {
		return result, err
	}

	state, found, err := snapshot.SemanticState()
	if err != nil {
		if semanticUnavailable(err) {
			return result, nil
		}

		return result, err
	}

	if !found || !hybrid.matchesState(state, manifest) {
		return result, nil
	}

	result.state = state

	result.dense, result.counts, err = snapshot.SearchSemantic(ctx, request, vectors, depth)
	if err != nil {
		if semanticUnavailable(err) {
			return result, nil
		}

		return result, err
	}

	result.certified = true

	return result, ctx.Err()
}

func (hybrid *Hybrid) matchesState(state semantic.State, manifest memory.ProjectionManifest) bool {
	return state.Status == semantic.StatusReady && state.PublicationEpoch > 0 &&
		semantic.ValidProjectionIdentity(state.BuildID) &&
		semantic.StateMatchesView(state, semantic.View{Scope: manifest.Scope,
			LexicalGenerationID: manifest.GenerationID, LexicalWatermark: manifest.Watermark,
			ModelSpace: hybrid.spaceID, Dimensions: hybrid.space.Dimensions})
}

func (hybrid *Hybrid) recertify(ctx context.Context, request memory.RecallRequest,
	before memory.ProjectionManifest, state semantic.State) (bool, error) {
	// A second short metadata snapshot also checks the exact internal physical
	// build binding, even if a replacement retains the same logical manifest.
	unchanged, err := hybrid.currentPair(ctx, request.Scope, before, state)
	if err != nil {
		return false, err
	}

	after, certified, err := hybrid.certifiedSource(ctx, request)
	if err != nil {
		return false, err
	}

	return unchanged && certified && before == after, ctx.Err()
}

func (hybrid *Hybrid) currentPair(ctx context.Context, scope memory.Scope,
	manifest memory.ProjectionManifest, before semantic.State) (unchanged bool, err error) {
	snapshot, err := hybrid.reader.OpenSearchSnapshot(ctx, scope)
	if err != nil {
		return false, err
	}
	defer func() {
		if closeErr := snapshot.Close(); err == nil {
			err = closeErr
		}
	}()

	state, found, err := snapshot.SemanticState()
	if err != nil {
		if semanticUnavailable(err) {
			return false, nil
		}

		return false, err
	}

	return found && sameHybridTuple(snapshot.LexicalManifest(), manifest) && state == before, nil
}

func sameHybridTuple(left, right memory.ProjectionManifest) bool {
	return left.Scope == right.Scope && left.ProjectionID == right.ProjectionID &&
		left.GenerationID == right.GenerationID && left.Watermark == right.Watermark && left.Status == right.Status
}

func semanticUnavailable(err error) bool {
	return errors.Is(err, semantic.ErrInvalid) || errors.Is(err, semantic.ErrCapacity) ||
		errors.Is(err, semantic.ErrConflict) || errors.Is(err, semantic.ErrCorrupt) || errors.Is(err, semantic.ErrClosed)
}

func (hybrid *Hybrid) fallback(ctx context.Context, request memory.RecallRequest,
	report *memory.RetrievalReport, reason string) ([]memory.RecallProjectionHit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if request.MinScopeChangeSeq != 0 {
		return nil, memory.RetryableError(memory.CodeConflict, "hybrid recall physical view is unavailable or changed", nil)
	}

	hits, err := hybrid.lexical.SearchRecall(ctx, request)
	if err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	hits = hits[:min(len(hits), semantic.FusedCandidateLimit)]
	report.Effective, report.Reason = "degraded", reason
	report.LexicalCandidates = min(len(hits), semantic.SemanticCandidateLimit)
	report.SemanticCandidates, report.ScannedChunks = 0, 0

	return hits, nil
}

func fuseHybridMatches(ctx context.Context, scope memory.Scope, lexical []memory.RecallProjectionHit,
	dense []semantic.DenseMatch) ([]memory.RecallProjectionHit, error) {
	lexical = lexical[:min(len(lexical), semantic.SemanticCandidateLimit)]
	dense = dense[:min(len(dense), semantic.SemanticCandidateLimit)]
	lexicalIDs, denseIDs := make([]string, len(lexical)), make([]string, len(dense))
	identities := make(map[string]memory.RecallProjectionHit, len(lexical)+len(dense))

	for i, match := range dense {
		denseIDs[i] = match.RevisionID
		identities[match.RevisionID] = memory.RecallProjectionHit{Scope: scope, RevisionID: match.RevisionID}
	}

	for i, hit := range lexical {
		lexicalIDs[i], identities[hit.RevisionID] = hit.RevisionID, hit
	}

	fused, err := semantic.Fuse(ctx, lexicalIDs, denseIDs, semantic.FusedCandidateLimit)
	if err != nil {
		return nil, err
	}

	hits := make([]memory.RecallProjectionHit, 0, len(fused))
	for _, match := range fused {
		hit := identities[match.RevisionID]
		hit.Score, hit.Lexical, hit.Semantic = match.Score, match.Lexical, match.Semantic
		hits = append(hits, hit)
	}

	return hits, nil
}

func hybridReadError(err error) error {
	if err == nil || err == context.Canceled || err == context.DeadlineExceeded {
		return err
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &hybridReadFailure{cause: err}
	}

	if _, _, classified := memory.ClassifyError(err); classified {
		return &hybridReadFailure{cause: err}
	}

	return &hybridReadFailure{cause: memory.PermanentError(memory.CodeStoreFailure,
		"hybrid recall storage is unavailable", err)}
}

// Retain the original classification and errors.Is chain without formatting
// private canonical/SQL diagnostics into the public error message.
type hybridReadFailure struct{ cause error }

func (*hybridReadFailure) Error() string { return "hybrid recall could not complete" }

func (failure *hybridReadFailure) Unwrap() error { return failure.cause }

var _ memory.RecallProjection = (*Hybrid)(nil)
var _ memory.ConsistentRecallProjection = (*Hybrid)(nil)
var _ memory.ReportedRecallProjection = (*Hybrid)(nil)
