package canonicalsql

import (
	"context"
	"strings"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

type boundaryBoundsResolver struct {
	ctx          context.Context
	txn          *Tx
	scope        memory.Scope
	at           time.Time
	cache        map[string]memory.BoundaryParentMetadata
	visiting     map[string]bool
	nodes, bytes int
	identities   map[string]*memory.CanonicalCompatibilityPayload
	revisions    map[string]memory.MemoryRevision
	sources      map[string]memory.CanonicalSourceRecord
}

func newBoundaryBoundsResolver(ctx context.Context,
	txn *Tx,
	scope memory.Scope,
	at time.Time) *boundaryBoundsResolver {
	return &boundaryBoundsResolver{ctx: ctx, txn: txn, scope: scope, at: at,
		cache:      make(map[string]memory.BoundaryParentMetadata),
		visiting:   make(map[string]bool),
		identities: make(map[string]*memory.CanonicalCompatibilityPayload),

		revisions: make(map[string]memory.MemoryRevision), sources: make(map[string]memory.CanonicalSourceRecord)}
}

func (r *boundaryBoundsResolver) resolve(ref memory.RevisionRef) (memory.BoundaryParentMetadata, error) {
	return r.resolveDepth(ref, 0)
}

func (r *boundaryBoundsResolver) resolveDepth(ref memory.RevisionRef,
	depth int) (memory.BoundaryParentMetadata,
	error) {
	if err := r.ctx.Err(); err != nil {
		return memory.BoundaryParentMetadata{}, err
	}

	if cached, ok := r.cache[ref.RevisionID]; ok {
		if ref.ItemID != "" && ref.ItemID != cached.CanonicalRef.ItemID {
			return memory.BoundaryParentMetadata{}, invalidBoundaryAncestry()
		}

		return r.ancestorObservedAt(cached, depth)
	}

	if r.visiting[ref.RevisionID] {
		return memory.BoundaryParentMetadata{}, invalidBoundaryAncestry()
	}

	if depth > memory.MaxSourcesPerRevision {
		return memory.BoundaryParentMetadata{},
			memory.PermanentError(memory.CodeLimitExceeded,
				"boundary ancestry exceeds the bounded proof budget",
				nil)
	}

	r.visiting[ref.RevisionID] = true
	metadata, err := r.resolveUncached(ref, depth)
	delete(r.visiting, ref.RevisionID)

	if err != nil {
		return memory.BoundaryParentMetadata{}, err
	}

	r.cache[ref.RevisionID] = metadata

	return r.ancestorObservedAt(metadata, depth)
}

func (r *boundaryBoundsResolver) resolveUncached(ref memory.RevisionRef,
	depth int) (memory.BoundaryParentMetadata,
	error) {
	revision, err := r.loadRevision(ref)
	if err != nil {
		return memory.BoundaryParentMetadata{}, err
	}

	own := memory.BoundaryParentMetadata{CanonicalRef: memory.RevisionRef{ItemID: revision.ItemID,
		RevisionID: revision.RevisionID},

		Temporal: cloneBoundaryTemporal(revision.Temporal), Sensitivity: revision.Sensitivity, Retention: revision.Retention}
	if revision.GroundingBoundsVersion == 1 {
		return own, nil
	}

	parents, err := r.legacySemanticParents(revision)
	if err != nil {
		return memory.BoundaryParentMetadata{}, err
	}

	if len(parents) == 0 {
		return own, nil
	}

	inherited := []memory.BoundaryParentMetadata{own}

	for _, id := range parents {
		parent, err := r.resolveDepth(memory.RevisionRef{RevisionID: id}, depth+1)
		if err != nil {
			return memory.BoundaryParentMetadata{}, err
		}

		inherited = append(inherited, parent)
	}
	// Including the existing revision's own bounds can add one record beyond
	// the direct-parent limit, so intersect its bounds separately.
	combined, err := memory.InheritBoundaryMetadata(own.Temporal.ObservedAt, inherited[1:])
	if err != nil {
		return memory.BoundaryParentMetadata{}, err
	}

	return mergeBoundaryAncestorMetadata(own, combined)
}

func mergeBoundaryAncestorMetadata(own, ancestor memory.BoundaryParentMetadata) (memory.BoundaryParentMetadata, error) {
	bounds, err := memory.IntersectGroundingBounds([]memory.Temporal{own.Temporal, ancestor.Temporal})
	if err != nil {
		return memory.BoundaryParentMetadata{}, err
	}

	own.Temporal.ValidFrom = bounds.ValidFrom
	own.Temporal.ValidUntil = bounds.ValidUntil

	own.Temporal.ExpiresAt = bounds.ExpiresAt
	if ancestor.Sensitivity == memory.SensitivitySensitive {
		own.Sensitivity = ancestor.Sensitivity
	}

	if ancestor.Retention == memory.RetentionClassEphemeral {
		own.Retention = ancestor.Retention
	}

	return own, nil
}

func (r *boundaryBoundsResolver) loadRevision(ref memory.RevisionRef) (memory.MemoryRevision, error) {
	if cached, ok := r.revisions[ref.RevisionID]; ok {
		if ref.ItemID != "" && ref.ItemID != cached.ItemID {
			return memory.MemoryRevision{}, invalidBoundaryAncestry()
		}

		return cached, nil
	}

	if r.nodes >= memory.MaxSnapshotItems {
		return memory.MemoryRevision{},
			memory.PermanentError(memory.CodeLimitExceeded,
				"boundary ancestry read budget exceeded",
				nil)
	}

	r.nodes++

	revision, err := loadProofRevision(r.ctx, r.txn, r.scope, ref.RevisionID)
	if err != nil {
		return memory.MemoryRevision{}, invalidBoundaryAncestry()
	}

	if ref.ItemID != "" && ref.ItemID != revision.ItemID {
		return memory.MemoryRevision{}, invalidBoundaryAncestry()
	}

	if err := checkProofDenial(r.ctx, r.txn, r.scope, DeniedRevision, revision.RevisionID); err != nil {
		return memory.MemoryRevision{}, invalidBoundaryAncestry()
	}

	if err := r.validateSourceMetadata(revision.Evidence); err != nil {
		return memory.MemoryRevision{}, err
	}

	r.revisions[revision.RevisionID] = revision

	return revision, nil
}

func (r *boundaryBoundsResolver) legacySemanticParents(revision memory.MemoryRevision) ([]string, error) {
	compatibility, err := r.legacyIdentity(revision)
	if err != nil {
		return nil, err
	}

	if compatibility == nil || compatibility.Kind == memory.DerivedKindAtom {
		return nil, nil
	}

	structural, err := r.legacyStructuralParent(revision, *compatibility)
	if err != nil {
		return nil, err
	}

	parents := make([]string, 0, len(revision.Parents))
	for _, parent := range revision.Parents {
		if parent != structural {
			parents = append(parents, parent)
		}
	}

	if len(parents) == 0 || len(parents) > memory.MaxSourcesPerRevision {
		return nil, invalidBoundaryAncestry()
	}

	return parents, nil
}

func (r *boundaryBoundsResolver) legacyIdentity(revision memory.MemoryRevision) (*memory.CanonicalCompatibilityPayload,
	error) {
	if identity, cached := r.identities[revision.RevisionID]; cached {
		return identity, nil
	}

	item, err := loadProofItem(r.ctx, r.txn, r.scope, revision.ItemID)
	if err != nil {
		return nil, invalidBoundaryAncestry()
	}

	if !strings.HasPrefix(string(item.MemoryKey), "derived:") {
		r.identities[revision.RevisionID] = nil

		return nil, nil
	}

	if uint64(revision.Payload.ByteSize) > uint64(memory.MaxDerivedTurnTextBytes-r.bytes) {
		return nil, memory.PermanentError(memory.CodeLimitExceeded, "boundary ancestry payload proof budget exceeded", nil)
	}

	payload, err := loadTurnIdentityPayload(r.ctx, r.txn, revision.Payload)
	if err != nil {
		return nil, invalidBoundaryAncestry()
	}

	r.bytes += len(payload)

	compatibility, ok := canonicalCompatibilityPayload(payload, r.scope)
	if !ok {
		return nil, invalidBoundaryAncestry()
	}

	r.identities[revision.RevisionID] = &compatibility

	return &compatibility, nil
}

func (r *boundaryBoundsResolver) legacyStructuralParent(revision memory.MemoryRevision,
	compatibility memory.CanonicalCompatibilityPayload) (string, error) {
	if compatibility.Supersedes == nil {
		return "", nil
	}

	var match string

	for _, id := range revision.Parents {
		parent, err := r.loadRevision(memory.RevisionRef{RevisionID: id})
		if err != nil {
			return "", err
		}

		identity, err := r.legacyIdentity(parent)
		if err != nil {
			return "", err
		}

		if identity != nil && identity.LegacyItemID == compatibility.Supersedes.ItemID &&
			identity.LegacyRevisionID == compatibility.Supersedes.RevisionID {
			if match != "" {
				return "", invalidBoundaryAncestry()
			}

			match = id
		}
	}

	if match == "" {
		return "", invalidBoundaryAncestry()
	}

	return match, nil
}

func (r *boundaryBoundsResolver) requireCurrent(ref memory.RevisionRef) error {
	if err := requireProofHead(r.ctx, r.txn, r.scope, ref); err != nil {
		return memory.PermanentError(memory.CodeConflict, "boundary grounding head changed", nil)
	}

	return nil
}

func invalidBoundaryAncestry() error {
	return memory.PermanentError(memory.CodeInvalidDerived, "boundary ancestor proof is unavailable or invalid", nil)
}

func (r *boundaryBoundsResolver) ancestorObservedAt(parent memory.BoundaryParentMetadata,
	depth int) (memory.BoundaryParentMetadata, error) {
	if depth > 0 && parent.Temporal.ObservedAt.After(r.at) {
		return memory.BoundaryParentMetadata{}, invalidBoundaryAncestry()
	}

	return parent, nil
}

func (r *boundaryBoundsResolver) validateSourceMetadata(evidenceRefs []memory.EvidenceRef) error {
	seen := make(map[string]struct{})
	for _, evidence := range evidenceRefs {
		if _, done := seen[evidence.SourceID]; done {
			continue
		}

		seen[evidence.SourceID] = struct{}{}
		if err := checkProofDenial(r.ctx, r.txn, r.scope, DeniedSource, evidence.SourceID); err != nil {
			return invalidBoundaryAncestry()
		}

		if err := r.loadSourceMetadata(evidence.SourceID); err != nil {
			return err
		}
	}

	return nil
}

func (r *boundaryBoundsResolver) loadSourceMetadata(sourceID string) error {
	if _, cached := r.sources[sourceID]; cached {
		return nil
	}

	if len(r.sources) >= memory.MaxSnapshotItems {
		return memory.PermanentError(memory.CodeLimitExceeded, "boundary ancestry source budget exceeded", nil)
	}

	key, err := privacyKey(r.scope, Source, sourceID)
	if err != nil {
		return err
	}

	var source memory.CanonicalSourceRecord
	if err := getPrivacyRecord(r.ctx, r.txn, key, Source, &source); err != nil {
		return invalidBoundaryAncestry()
	}

	if err := source.Validate(); err != nil || source.Scope != r.scope {
		return invalidBoundaryAncestry()
	}

	r.sources[sourceID] = source

	return nil
}
