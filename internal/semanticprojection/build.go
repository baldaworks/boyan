package semanticprojection

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	semanticcore "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

type buildTarget struct {
	manifest        memory.ProjectionManifest
	generation      semanticcore.Generation
	expectedBuildID string
	expectedEpoch   uint64
}

type reusableKey struct {
	itemID      string
	revision    uint64
	revisionID  string
	contentHash string
	inputHash   string
	ordinal     uint32
}

//nolint:cyclop,funlen,nlreturn,wsl_v5 // Publication gates intentionally remain explicit and adjacent.
func (manager *Manager) build(ctx context.Context, scope memory.Scope, current *flight) (buildErr error) {
	if manager.beforeBuild != nil {
		manager.beforeBuild(ctx, scope)
	}
	if err := ctx.Err(); err != nil {
		manager.recordBuildFailure(scope, buildTarget{}, err)
		return err
	}
	buildAt := manager.now().UTC()
	current.snapshotEpoch = manager.currentRequestEpoch()

	target, alreadyReady, err := manager.loadTarget(ctx, scope, buildAt)
	if err != nil {
		manager.recordBuildFailure(scope, target, err)
		return err
	}
	if alreadyReady {
		if manager.beforeFinalCheck != nil {
			manager.beforeFinalCheck()
		}
		if err := manager.verifyBuildTarget(ctx, target); err != nil {
			manager.recordBuildFailure(scope, target, err)
			return err
		}
		manager.recordReady(scope, target.generation)
		return nil
	}

	manager.setStatus(scope, ScopeStatus{
		State: StateRepairing, LexicalGenerationID: target.generation.LexicalGenerationID,
		LexicalWatermark: target.generation.LexicalWatermark, ModelSpace: manager.spaceID,
		UpdatedAt: buildAt,
	})

	reusable, err := manager.loadReusable(ctx, scope)
	if err != nil {
		manager.recordBuildFailure(scope, target, err)
		return err
	}

	coverage, chunks, inputs, omitted, err := manager.prepareScope(ctx, target.generation, reusable, buildAt)
	if err != nil {
		manager.publishFailure(ctx, target, err)
		manager.recordBuildFailure(scope, target, err)
		return err
	}
	target.generation.Omitted = omitted
	if _, err := semanticcore.PreflightStage(ctx, target.generation, coverage, chunks); err != nil {
		manager.publishFailure(ctx, target, err)
		manager.recordBuildFailure(scope, target, err)
		return err
	}
	if err := manager.embedMissing(ctx, chunks, inputs); err != nil {
		manager.publishFailure(ctx, target, err)
		manager.recordBuildFailure(scope, target, err)
		return err
	}

	// Retain our generated identity, even when Stage durably claims and then
	// returns no State. This terminal attempt finishes before the flight, slot
	// and producer drain ownership can be released by runSingleFlight.
	stageFailed := false
	defer func() {
		manager.finishBuild(ctx, target, buildErr, stageFailed)
	}()
	if _, err := manager.store.Stage(ctx, target.generation, coverage, chunks); err != nil {
		stageFailed = true
		return fmt.Errorf("stage semantic projection: %w", err)
	}

	if manager.beforeFinalCheck != nil {
		manager.beforeFinalCheck()
	}

	if err := manager.verifyBuildTarget(ctx, target); err != nil {
		manager.recordBuildFailure(scope, target, err)
		return err
	}
	if manager.beforePublish != nil {
		manager.beforePublish()
	}

	err = manager.store.Publish(ctx, semanticcore.Publication{
		View: target.generation.View(), BuildID: target.generation.BuildID,
		ExpectedActiveBuildID: target.expectedBuildID, ExpectedActiveEpoch: target.expectedEpoch,
	})
	if err != nil {
		manager.recordBuildFailure(scope, target, err)
		return fmt.Errorf("publish semantic projection: %w", err)
	}
	if manager.afterPublish != nil {
		manager.afterPublish()
	}
	if err := manager.verifyBuildTarget(ctx, target); err != nil {
		manager.recordBuildFailure(scope, target, err)
		return err
	}

	manager.recordReady(scope, target.generation)
	return nil
}

//nolint:cyclop,nlreturn,wsl_v5 // Exact canonical, lexical, and semantic compatibility gates are explicit.
func (manager *Manager) loadTarget(
	ctx context.Context,
	scope memory.Scope,
	buildAt time.Time,
) (buildTarget, bool, error) {
	manifest, err := manager.loadCertifiedSource(ctx, scope)
	if err != nil {
		return buildTarget{}, false, err
	}

	state, err := manager.canonical.LoadScopeState(ctx, scope)
	if err != nil {
		return buildTarget{}, false, err
	}
	if state.Scope != scope || state.Validate() != nil || state.ChangeSeq != manifest.Watermark {
		return buildTarget{}, false, ErrStaleView
	}

	buildID, err := newBuildID()
	if err != nil {
		return buildTarget{}, false, err
	}

	target := buildTarget{manifest: manifest, generation: semanticcore.Generation{
		Scope: scope, BuildID: buildID, LexicalGenerationID: manifest.GenerationID,
		LexicalWatermark: manifest.Watermark, ModelSpace: manager.spaceID,
		Dimensions: manager.space.Dimensions, ReadyAt: buildAt,
	}}

	active, activeFound, err := manager.store.ActiveState(ctx, scope)
	if err != nil {
		return buildTarget{}, false, err
	}
	if !activeFound {
		return target, false, nil
	}
	target.expectedBuildID = active.BuildID
	target.expectedEpoch = active.PublicationEpoch
	if active.Status != semanticcore.StatusReady || active.View() != target.generation.View() {
		return target, false, nil
	}
	ready, err := manager.store.ObservedMetadataReady(ctx, active.View())
	if err != nil {
		return buildTarget{}, false, err
	}
	if !ready {
		return target, false, nil
	}
	target.generation.BuildID = active.BuildID
	target.generation.ReadyAt = active.ReadyAt
	return target, true, nil
}

//nolint:wsl_v5 // The snapshot gate and bounded copy form one cohesive reuse operation.
func (manager *Manager) loadReusable(
	ctx context.Context,
	scope memory.Scope,
) (map[reusableKey][]float32, error) {
	active, found, err := manager.store.ActiveState(ctx, scope)
	if err != nil || !found || active.Status != semanticcore.StatusReady ||
		active.ModelSpace != manager.spaceID || active.Dimensions != manager.space.Dimensions {
		return nil, err
	}

	reusable, compatible, err := manager.store.LoadReusable(ctx, active.View())
	if err != nil || !compatible {
		return nil, err
	}

	result := make(map[reusableKey][]float32, int(active.ChunkCount))
	for _, chunk := range reusable.Chunks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result[reusableKey{
			itemID: chunk.ItemID, revision: chunk.Revision,
			revisionID: chunk.RevisionID, contentHash: chunk.ContentHash,
			inputHash: chunk.InputHash, ordinal: chunk.Ordinal,
		}] = append([]float32(nil), chunk.Vector...)
	}

	return result, nil
}

//nolint:cyclop,funlen,wsl_v5 // Paging, cancellation, and capacity gates are independently auditable.
func (manager *Manager) prepareScope(
	ctx context.Context,
	generation semanticcore.Generation,
	reusable map[reusableKey][]float32,
	buildAt time.Time,
) ([]semanticcore.CoverageRecord, []semanticcore.ProjectionChunk, []string, semanticcore.OmittedCounts, error) {
	var (
		coverage []semanticcore.CoverageRecord
		chunks   []semanticcore.ProjectionChunk
		inputs   []string
		omitted  semanticcore.OmittedCounts
		after    string
		scanned  int
		pages    int
	)

	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, omitted, err
		}

		state, err := manager.canonical.LoadScopeState(ctx, generation.Scope)
		if err != nil {
			return nil, nil, nil, omitted, err
		}
		if state.Scope != generation.Scope || state.Validate() != nil || state.ChangeSeq != generation.LexicalWatermark {
			return nil, nil, nil, omitted, ErrStaleView
		}

		page, err := manager.canonical.ScanActiveMemory(ctx, memory.ActiveMemoryScanRequest{
			Scope: generation.Scope, AfterItemID: after, Limit: canonicalPageSize,
		})
		if err != nil {
			return nil, nil, nil, omitted, err
		}
		pages++
		if len(page) > canonicalPageSize || pages > projectionScanPageLimit ||
			scanned+len(page) > semanticcore.ProjectionScanRecordLimit {
			return nil, nil, nil, omitted, semanticcore.ErrCapacity
		}
		if len(page) == 0 {
			break
		}
		if err := validateActivePage(generation.Scope, after, page); err != nil {
			return nil, nil, nil, omitted, err
		}
		scanned += len(page)

		pageCoverage, pageChunks, pageInputs, pageOmitted, err := manager.preparePage(
			ctx, generation, page, reusable, buildAt,
		)
		if err != nil {
			return nil, nil, nil, omitted, err
		}
		coverage = append(coverage, pageCoverage...)
		chunks = append(chunks, pageChunks...)
		inputs = append(inputs, pageInputs...)
		omitted.Ineligible += pageOmitted.Ineligible
		omitted.Sensitive += pageOmitted.Sensitive
		omitted.Expired += pageOmitted.Expired

		if len(chunks) > semanticcore.ProjectionChunkLimit {
			return nil, nil, nil, omitted, semanticcore.ErrCapacity
		}

		after = page[len(page)-1].Item.ItemID
		if len(page) < canonicalPageSize {
			break
		}
	}

	return coverage, chunks, inputs, omitted, nil
}

//nolint:cyclop,funlen,gocognit,nlreturn,wsl_v5 // Privacy, identity, coverage, and reuse checks stay adjacent.
func (manager *Manager) preparePage(
	ctx context.Context,
	generation semanticcore.Generation,
	page []memory.ActiveCanonicalMemory,
	reusable map[reusableKey][]float32,
	buildAt time.Time,
) ([]semanticcore.CoverageRecord, []semanticcore.ProjectionChunk, []string, semanticcore.OmittedCounts, error) {
	ids := make([]string, 0, len(page))
	activeByRevision := make(map[string]memory.ActiveCanonicalMemory, len(page))
	for _, active := range page {
		if active.Item.Scope != generation.Scope || active.RevisionID == "" {
			return nil, nil, nil, semanticcore.OmittedCounts{}, ErrStaleView
		}
		ids = append(ids, active.RevisionID)
		activeByRevision[active.RevisionID] = active
	}

	records, err := manager.records.LoadRecallRecords(ctx, generation.Scope, ids)
	if err != nil {
		return nil, nil, nil, semanticcore.OmittedCounts{}, err
	}
	if len(records) > len(page) {
		return nil, nil, nil, semanticcore.OmittedCounts{}, ErrStaleView
	}

	coverage := make([]semanticcore.CoverageRecord, 0, len(records))
	chunks := make([]semanticcore.ProjectionChunk, 0, len(records))
	inputs := make([]string, 0, len(records))
	omitted := semanticcore.OmittedCounts{Ineligible: uint32(len(page) - len(records))}
	seen := make(map[string]struct{}, len(records))

	for _, record := range records {
		active, found := activeByRevision[record.RevisionID]
		if !found || record.Scope != generation.Scope || record.ItemID != active.Item.ItemID ||
			record.Revision != active.Revision || record.ScopeChangeSeq != generation.LexicalWatermark {
			return nil, nil, nil, omitted, ErrStaleView
		}
		if _, duplicate := seen[record.RevisionID]; duplicate {
			return nil, nil, nil, omitted, ErrStaleView
		}
		seen[record.RevisionID] = struct{}{}

		if record.State != memory.RevisionStateActive {
			omitted.Ineligible++
			continue
		}
		if record.Sensitivity != memory.SensitivityStandard {
			omitted.Sensitive++
			continue
		}
		if record.Retention != memory.RetentionClassStandard && record.Retention != memory.RetentionClassEphemeral {
			omitted.Ineligible++
			continue
		}
		if err := record.Temporal.Validate(); err != nil {
			return nil, nil, nil, omitted, err
		}
		if !memory.TemporalEligibleAt(record.Temporal, buildAt) {
			if temporalExpiredAt(record.Temporal, buildAt) {
				omitted.Expired++
			} else {
				omitted.Ineligible++
			}
			continue
		}
		if err := projectionDocument(record).Validate(); err != nil {
			return nil, nil, nil, omitted, err
		}

		prepared, err := semanticcore.PreparePassage(ctx, record.Text, manager.space.PassagePrefix)
		if err != nil {
			return nil, nil, nil, omitted, err
		}
		if len(chunks)+len(prepared.Windows) > semanticcore.ProjectionChunkLimit {
			return nil, nil, nil, omitted, semanticcore.ErrCapacity
		}

		contentDigest := sha256.Sum256([]byte(record.Text))
		contentHash := hex.EncodeToString(contentDigest[:])
		category := memory.AtomCategory("")
		if record.Category != nil {
			category = *record.Category
		}
		for ordinal, window := range prepared.Windows {
			chunk := semanticcore.ProjectionChunk{
				Scope: generation.Scope, BuildID: generation.BuildID,
				LexicalGenerationID: generation.LexicalGenerationID,
				LexicalWatermark:    generation.LexicalWatermark,
				ModelSpace:          generation.ModelSpace, Dimensions: generation.Dimensions,
				ItemID: record.ItemID, RevisionID: record.RevisionID, Revision: record.Revision,
				Ordinal: uint32(ordinal), ContentHash: contentHash, InputHash: window.ContentHash,
				ScopeChangeSeq: record.ScopeChangeSeq, Kind: record.Kind, Category: category,
				MemoryKey: record.MemoryKey, CreatedAt: record.CreatedAt,
				ObservedAt: cloneTime(&record.Temporal.ObservedAt),
				ValidFrom:  cloneTime(record.Temporal.ValidFrom), ValidUntil: cloneTime(record.Temporal.ValidUntil),
				ExpiresAt:       cloneTime(record.Temporal.ExpiresAt),
				SourceIDs:       append([]string(nil), record.SourceIDs...),
				ConversationIDs: append([]string(nil), record.ConversationIDs...),
			}
			if vector, ok := reusable[reusableKey{
				itemID: record.ItemID, revision: record.Revision,
				revisionID: record.RevisionID, contentHash: contentHash,
				inputHash: window.ContentHash, ordinal: uint32(ordinal),
			}]; ok {
				chunk.Vector = append([]float32(nil), vector...)
			}
			chunks = append(chunks, chunk)
			inputs = append(inputs, window.Input)
		}
		coverage = append(coverage, semanticcore.CoverageRecord{
			ItemID: record.ItemID, RevisionID: record.RevisionID, Revision: record.Revision,
			ContentHash: contentHash, ChunkCount: uint32(len(prepared.Windows)),
		})
	}

	return coverage, chunks, inputs, omitted, nil
}

func temporalExpiredAt(temporal memory.Temporal, at time.Time) bool {
	return temporal.ExpiresAt != nil && !at.Before(*temporal.ExpiresAt) ||
		temporal.ValidUntil != nil && !at.Before(*temporal.ValidUntil)
}

func projectionDocument(record memory.RecallRecord) memory.RecallProjectionDocument {
	return memory.RecallProjectionDocument{
		Scope: record.Scope, ItemID: record.ItemID, RevisionID: record.RevisionID,
		Revision: record.Revision, Kind: record.Kind, Category: record.Category,
		MemoryKey: record.MemoryKey, Text: record.Text, CreatedAt: record.CreatedAt,
		Temporal: record.Temporal, Sensitivity: record.Sensitivity, Retention: record.Retention,
		SourceIDs: record.SourceIDs, ConversationIDs: record.ConversationIDs,
		ScopeChangeSeq: record.ScopeChangeSeq,
	}
}

//nolint:wsl_v5 // Batch construction and validated assignment remain adjacent.
func (manager *Manager) embedMissing(
	ctx context.Context,
	chunks []semanticcore.ProjectionChunk,
	preparedInputs []string,
) error {
	if len(chunks) != len(preparedInputs) {
		return &semanticcore.Error{Reason: semanticcore.ReasonInvalidResponse}
	}

	missing := make([]int, 0, len(chunks))
	for index := range chunks {
		if len(chunks[index].Vector) == 0 {
			missing = append(missing, index)
		}
	}

	for start := 0; start < len(missing); start += semanticcore.EmbeddingBatchLimit {
		end := min(start+semanticcore.EmbeddingBatchLimit, len(missing))
		inputs := make([]string, end-start)
		for offset, chunkIndex := range missing[start:end] {
			inputs[offset] = preparedInputs[chunkIndex]
		}

		vectors, err := manager.provider.Embed(ctx, inputs)
		if err != nil {
			return err
		}
		if len(vectors) != len(inputs) {
			return &semanticcore.Error{Reason: semanticcore.ReasonInvalidResponse}
		}

		for offset, vector := range vectors {
			if err := semanticcore.ValidateVector(vector, manager.space.Dimensions); err != nil {
				return err
			}
			chunks[missing[start+offset]].Vector = append([]float32(nil), vector...)
		}
	}

	return nil
}

//nolint:wsl_v5 // The canonical and lexical samples form one exact comparison.
func (manager *Manager) verifyTarget(ctx context.Context, before memory.ProjectionManifest) error {
	state, err := manager.canonical.LoadScopeState(ctx, before.Scope)
	if err != nil {
		return err
	}
	after, err := manager.loadCertifiedSource(ctx, before.Scope)
	if err != nil {
		return err
	}
	if state.Scope != before.Scope || state.Validate() != nil || state.ChangeSeq != before.Watermark || after != before {
		return ErrStaleView
	}

	return nil
}

func (manager *Manager) verifyBuildTarget(ctx context.Context, target buildTarget) error {
	if target.generation.ModelSpace != manager.spaceID || target.generation.Dimensions != manager.space.Dimensions {
		return ErrStaleView
	}

	return manager.verifyTarget(ctx, target.manifest)
}

func (manager *Manager) loadCertifiedSource(
	ctx context.Context,
	scope memory.Scope,
) (memory.ProjectionManifest, error) {
	durable, found, err := manager.manifests.LoadActiveProjectionManifest(ctx, scope, manager.lexicalID)
	if err != nil {
		return memory.ProjectionManifest{}, err
	}

	physical, physicallyActive, err := manager.physical.LoadActivePhysicalProjectionManifest(
		ctx, scope, manager.lexicalID,
	)
	if err != nil {
		return memory.ProjectionManifest{}, err
	}

	if !found || !physicallyActive || durable != physical || durable.Scope != scope ||
		durable.ProjectionID != manager.lexicalID || durable.Status != memory.ProjectionGenerationActive ||
		durable.Validate() != nil {
		return memory.ProjectionManifest{}, ErrStaleView
	}

	return durable, nil
}

func (manager *Manager) verifyView(ctx context.Context, view semanticcore.View) error {
	manifest, err := manager.loadCertifiedSource(ctx, view.Scope)
	if err != nil {
		return err
	}

	state, err := manager.canonical.LoadScopeState(ctx, view.Scope)
	if err != nil {
		return err
	}

	if state.Scope != view.Scope || state.Validate() != nil || state.ChangeSeq != manifest.Watermark ||
		view.LexicalGenerationID != manifest.GenerationID || view.LexicalWatermark != manifest.Watermark ||
		view.ModelSpace != manager.spaceID || view.Dimensions != manager.space.Dimensions {
		return ErrStaleView
	}

	return nil
}

func validateActivePage(scope memory.Scope, after string, page []memory.ActiveCanonicalMemory) error {
	previous := after
	revisions := make(map[string]struct{}, len(page))

	for _, active := range page {
		if active.Item.Scope != scope || active.Item.Validate() != nil || active.RevisionID == "" ||
			active.Revision == 0 || active.Item.ItemID <= previous {
			return ErrStaleView
		}

		if _, duplicate := revisions[active.RevisionID]; duplicate {
			return ErrStaleView
		}

		revisions[active.RevisionID] = struct{}{}
		previous = active.Item.ItemID
	}

	return nil
}

func (manager *Manager) publishFailure(ctx context.Context, target buildTarget, buildErr error) {
	reason, ok := classifyFailure(buildErr)
	if !ok || ctx.Err() != nil || target.generation.BuildID == "" || manager.verifyTarget(ctx, target.manifest) != nil {
		return
	}
	// Diagnostic markers never reuse a candidate identity, including after a
	// failed Stage that was already released by the terminal guard.
	markerID, err := newBuildID()
	if err != nil {
		return
	}

	target.generation.BuildID = markerID

	_, _ = manager.store.PublishDegraded(ctx, semanticcore.Degraded{
		Generation: target.generation, SafeReason: reason, ExpectedActiveEpoch: target.expectedEpoch,
	}, target.expectedBuildID)
}

//nolint:wsl_v5 // Each stable degradation class is intentionally independent.
func classifyFailure(err error) (semanticcore.Reason, bool) {
	if reason, ok := semanticcore.Classify(err); ok {
		return reason, true
	}
	if errors.Is(err, semanticcore.ErrCapacity) || errors.Is(err, ErrQueueFull) {
		return semanticcore.ReasonProjectionCapacity, true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return semanticcore.ReasonDeadline, true
	}
	if errors.Is(err, ErrStaleView) || errors.Is(err, semanticcore.ErrConflict) {
		return semanticcore.ReasonUnavailable, true
	}

	return semanticcore.ReasonUnavailable, true
}

func (manager *Manager) recordBuildFailure(scope memory.Scope, target buildTarget, err error) {
	reason, _ := classifyFailure(err)
	manager.setStatus(scope, ScopeStatus{
		State: StateDegraded, SafeReason: reason,
		LexicalGenerationID: target.generation.LexicalGenerationID,
		LexicalWatermark:    target.generation.LexicalWatermark,
		ModelSpace:          manager.spaceID, UpdatedAt: manager.now().UTC(),
	})
}

func (manager *Manager) recordReady(scope memory.Scope, generation semanticcore.Generation) {
	manager.setStatus(scope, ScopeStatus{
		State: StateReady, LexicalGenerationID: generation.LexicalGenerationID,
		LexicalWatermark: generation.LexicalWatermark, ModelSpace: generation.ModelSpace,
		UpdatedAt: generation.ReadyAt,
	})
}

//nolint:nlreturn,wsl_v5 // A separate local makes pointer ownership explicit.
func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

//nolint:nlreturn,wsl_v5 // The successful return directly encodes the generated bytes.
func newBuildID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("create semantic build identity: %w", err)
	}
	return "semantic-" + hex.EncodeToString(value[:]), nil
}
