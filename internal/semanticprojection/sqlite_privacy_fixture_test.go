package semanticprojection //nolint:testpackage // Component fixtures share accepted fake canonical records.

import (
	"context"
	"sync"

	"github.com/baldaworks/boyan/pkg/memory"
)

// SQL component authority has explicit synthetic structures independent of
// hydration availability. The known history survives fake head replacement.
type sqliteFixturePrivacy struct {
	mu        sync.Mutex
	canonical *fakeCanonical
	revisions map[string]memory.MemoryRevision
	sources   map[string]bool
}

func (p *sqliteFixturePrivacy) IsRevisionDenied(ctx context.Context, scope memory.Scope, id string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.importKnown()

	if _, known := p.revisions[id]; scope != p.canonical.scope || !known {
		return false, memory.PermanentError(memory.CodeNotFound, "unknown SQL fixture revision", nil)
	}

	return false, ctx.Err()
}

func (p *sqliteFixturePrivacy) IsSourceDenied(ctx context.Context, scope memory.Scope, id string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.importKnown()

	if scope != p.canonical.scope || !p.sources[id] {
		return false, memory.PermanentError(memory.CodeNotFound, "unknown SQL fixture source", nil)
	}

	return false, ctx.Err()
}

func (p *sqliteFixturePrivacy) LoadCanonicalRevisions(ctx context.Context,
	request memory.CanonicalRevisionReadRequest) ([]memory.MemoryRevision, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.importKnown()

	var result []memory.MemoryRevision

	for _, id := range request.RevisionIDs {
		revision, known := p.revisions[id]
		if request.Scope != p.canonical.scope || !known {
			return nil, memory.PermanentError(memory.CodeNotFound, "unknown SQL fixture structure", nil)
		}

		result = append(result, revision)
	}

	return result, ctx.Err()
}

func (p *sqliteFixturePrivacy) importKnown() {
	p.canonical.mu.Lock()
	defer p.canonical.mu.Unlock()

	for id, record := range p.canonical.records {
		revision := memory.MemoryRevision{RevisionID: id, ItemID: record.ItemID, Revision: record.Revision}
		for _, source := range record.SourceIDs {
			revision.Evidence = append(revision.Evidence, memory.EvidenceRef{SourceID: source})
			p.sources[source] = true
		}

		p.revisions[id] = revision
	}
}
