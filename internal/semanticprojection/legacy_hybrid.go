package semanticprojection

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/baldaworks/boyan/internal/embedding"
	semanticcore "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

// HybridLexical is the required lexical adapter and its physical certificate.
type HybridLexical interface {
	memory.RecallProjection
	memory.CertifiedRecallProjection
	memory.PhysicalProjectionViewReader
}

// LegacyHybrid preserves the pre-cutover runtime until SQLite composition is
// installed. It is deliberately separate from the SQL-only Hybrid consumer.
type LegacyHybrid struct {
	lexical  HybridLexical
	store    LegacySearchReader
	provider embedding.Provider
	space    semanticcore.ModelSpace
	spaceID  string
}

// NewLegacyHybrid is the temporary constructor for existing legacy composition.
func NewLegacyHybrid(lexical HybridLexical, store LegacySearchReader, provider embedding.Provider,
	space semanticcore.ModelSpace) (*LegacyHybrid, error) {
	spaceID, err := semanticcore.SpaceFingerprint(space)
	if err != nil {
		return nil, err
	}

	if lexical == nil || store == nil || provider == nil {
		return nil, fmt.Errorf("hybrid dependencies are required")
	}
	// Preserve rejection of a legacy nil *Store wrapped by the read port.
	if value := reflect.ValueOf(store); value.Kind() == reflect.Pointer && value.IsNil() {
		return nil, fmt.Errorf("hybrid dependencies are required")
	}

	return &LegacyHybrid{lexical: lexical, store: store, provider: provider, space: space, spaceID: spaceID}, nil
}

// SearchRecall returns independently ranked and fused candidates before caller truncation.
func (hybrid *LegacyHybrid) SearchRecall(ctx context.Context,
	request memory.RecallRequest) ([]memory.RecallProjectionHit, error) {
	hits, _, err := hybrid.SearchRecallWithReport(ctx, request)

	return hits, err
}

// SearchRecallWithReport returns immutable per-request, text-free diagnostics.
func (hybrid *LegacyHybrid) SearchRecallWithReport(ctx context.Context,
	request memory.RecallRequest) ([]memory.RecallProjectionHit, *memory.RetrievalReport, error) {
	report := &memory.RetrievalReport{Requested: "hybrid", Effective: "hybrid", ModelSpacePrefix: hybrid.spaceID[:12]}

	hits, err := hybrid.search(ctx, request, report)
	if err != nil {
		return nil, nil, err
	}

	return hits, report, nil
}

// SearchRecallConsistent requires the same physical certificate for both channels,
// including an empty result. It shares SearchRecall's minimum-watermark policy.
func (hybrid *LegacyHybrid) SearchRecallConsistent(ctx context.Context,
	request memory.RecallRequest) ([]memory.RecallProjectionHit, error) {
	return hybrid.SearchRecall(ctx, request)
}

//nolint:cyclop,funlen // Each channel failure has an explicit fail-closed minimum/cancellation policy.
func (hybrid *LegacyHybrid) search(ctx context.Context,
	request memory.RecallRequest, report *memory.RetrievalReport) ([]memory.RecallProjectionHit, error) {
	request, err := normalizeHybridRequest(ctx, request)
	if err != nil {
		return nil, err
	}

	// Direct callers also pin eligibility before lexical or provider work.
	if request.AsOf == nil {
		at := time.Now().UTC()
		request.AsOf = &at
	}

	prepared, err := semanticcore.PrepareQuery(ctx, request.Query, hybrid.space.QueryPrefix)
	if err != nil {
		return nil, err
	}

	report.OmittedQueryRunes = prepared.OmittedRunes
	report.OmittedQueryWindows = prepared.OmittedWindows

	depth := semanticcore.CandidateLimit(request.Limit)

	lexical, err := hybrid.lexical.SearchRecallCertified(ctx, request, depth)
	if err != nil {
		return nil, err
	}

	if !lexical.Certified {
		return hybrid.fallbackReport(ctx, request, report, "unavailable")
	}

	view := semanticcore.View{Scope: request.Scope, LexicalGenerationID: lexical.Manifest.GenerationID,
		LexicalWatermark: lexical.Manifest.Watermark, ModelSpace: hybrid.spaceID, Dimensions: hybrid.space.Dimensions}

	snapshot, found, err := hybrid.store.Snapshot(ctx, view)
	if err != nil || !found {
		return hybrid.fallbackReport(ctx, request, report, "unavailable")
	}

	vectors, err := hybrid.embedQuery(ctx, prepared)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}

		return hybrid.fallbackReport(ctx, request, report, safeRecallReason(err))
	}

	hits, err := hybrid.fuse(ctx, request, lexical.Hits, vectors, snapshot.Chunks(), depth)
	if err != nil {
		return hybrid.fallbackReport(ctx, request, report, safeRecallReason(err))
	}

	report.LexicalCandidates = min(len(lexical.Hits), depth)
	report.ScannedChunks = len(snapshot.Chunks())

	for _, hit := range hits {
		if hit.Semantic != 0 {
			report.SemanticCandidates++
		}
	}

	after, certified, err := hybrid.lexical.LoadActivePhysicalProjectionManifest(
		ctx, request.Scope, lexical.Manifest.ProjectionID)
	if err != nil {
		return nil, err
	}

	if !certified || after != lexical.Manifest {
		return hybrid.fallbackReport(ctx, request, report, "unavailable")
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return hits, nil
}

func (hybrid *LegacyHybrid) fallbackReport(ctx context.Context, request memory.RecallRequest,
	report *memory.RetrievalReport, reason string) ([]memory.RecallProjectionHit, error) {
	report.Effective, report.Reason = "degraded", reason
	report.SemanticCandidates, report.ScannedChunks = 0, 0
	hits, err := hybrid.fallback(ctx, request)
	report.LexicalCandidates = min(len(hits), semanticcore.SemanticCandidateLimit)

	return hits, err
}

func (hybrid *LegacyHybrid) embedQuery(ctx context.Context, prepared semanticcore.PreparedText) ([][]float32, error) {
	inputs := make([]string, len(prepared.Windows))
	for i, window := range prepared.Windows {
		inputs[i] = window.Input
	}

	vectors, err := hybrid.provider.Embed(ctx, inputs)
	if err != nil {
		return nil, err
	}

	if len(vectors) != len(inputs) {
		return nil, &embedding.Error{Code: embedding.CodeInvalidResponse}
	}

	for _, vector := range vectors {
		if err := semanticcore.ValidateVector(vector, hybrid.space.Dimensions); err != nil {
			return nil, err
		}
	}

	return vectors, nil
}

func (hybrid *LegacyHybrid) fallback(ctx context.Context,
	request memory.RecallRequest) ([]memory.RecallProjectionHit, error) {
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

	return hits[:min(len(hits), semanticcore.FusedCandidateLimit)], nil
}

//nolint:cyclop,funlen // Bounded prefiltering, ranking, and identity assembly preserve independent channel order.
func (hybrid *LegacyHybrid) fuse(ctx context.Context, request memory.RecallRequest,
	lexical []memory.RecallProjectionHit, vectors [][]float32, chunks []semanticcore.ProjectionChunk,
	depth int) ([]memory.RecallProjectionHit, error) {
	if len(chunks) > semanticcore.ProjectionChunkLimit {
		return nil, semanticcore.ErrCapacity
	}

	eligible := make([]semanticcore.Chunk, 0, len(chunks))
	identityCapacity := min(len(chunks)+len(lexical), semanticcore.ProjectionChunkLimit)
	identities := make(map[string]memory.RecallProjectionHit, identityCapacity)

	effective := time.Now().UTC()
	if request.AsOf != nil {
		effective = request.AsOf.UTC()
	}

	for _, chunk := range chunks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if chunk.ObservedAt == nil || chunk.ObservedAt.IsZero() {
			return nil, semanticcore.ErrCorrupt
		}

		if !hybridChunkEligible(chunk, request, effective) {
			continue
		}

		eligible = append(eligible, semanticcore.Chunk{
			RevisionID: chunk.RevisionID, Ordinal: int(chunk.Ordinal), Vector: chunk.Vector})
		identities[chunk.RevisionID] = memory.RecallProjectionHit{Scope: chunk.Scope, ItemID: chunk.ItemID,
			RevisionID: chunk.RevisionID, Revision: chunk.Revision, ScopeChangeSeq: chunk.ScopeChangeSeq}
	}

	dense, err := semanticcore.Rank(ctx, vectors, eligible, depth)
	if err != nil {
		return nil, err
	}

	lexicalIDs := make([]string, 0, min(len(lexical), depth))
	for _, hit := range lexical[:min(len(lexical), depth)] {
		lexicalIDs = append(lexicalIDs, hit.RevisionID)
		identities[hit.RevisionID] = hit
	}

	denseIDs := make([]string, len(dense))
	for i, match := range dense {
		denseIDs[i] = match.RevisionID
	}

	fused, err := semanticcore.Fuse(ctx, lexicalIDs, denseIDs, semanticcore.FusedCandidateLimit)
	if err != nil {
		return nil, err
	}

	hits := make([]memory.RecallProjectionHit, 0, len(fused))
	for _, match := range fused {
		hit := identities[match.RevisionID]
		hit.Score = match.Score
		hit.Lexical, hit.Semantic = match.Lexical, match.Semantic
		hits = append(hits, hit)
	}

	return hits, nil
}

//nolint:cyclop // Every caller filter is checked independently before cosine work.
func hybridChunkEligible(chunk semanticcore.ProjectionChunk, request memory.RecallRequest, at time.Time) bool {
	if chunk.Scope != request.Scope || request.Kind != nil && chunk.Kind != *request.Kind ||
		request.Category != nil && chunk.Category != *request.Category ||
		request.MemoryKey != "" && chunk.MemoryKey != request.MemoryKey {
		return false
	}

	if request.SourceID != "" && !hasString(chunk.SourceIDs, request.SourceID) ||
		request.ConversationID != "" && !hasString(chunk.ConversationIDs, request.ConversationID) {
		return false
	}

	return memory.TemporalEligibleAt(memory.Temporal{ObservedAt: *chunk.ObservedAt, ValidFrom: chunk.ValidFrom,
		ValidUntil: chunk.ValidUntil, ExpiresAt: chunk.ExpiresAt}, at)
}

func hasString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}

	return false
}

var _ memory.RecallProjection = (*LegacyHybrid)(nil)
var _ memory.ConsistentRecallProjection = (*LegacyHybrid)(nil)
