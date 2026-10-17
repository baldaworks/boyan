package sqlite

import (
	"context"
	"sync"
	"testing"

	"github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

type fixturePrivacyIdentity struct {
	scope memory.Scope
	id    string
}

// Explicit synthetic structural authority, not an allow-all dependency. Raw
// material fixture helpers register identities; unknown lookups are errors.
type fixturePrivacy struct {
	mu        sync.Mutex
	revisions map[fixturePrivacyIdentity]memory.MemoryRevision
	sources   map[fixturePrivacyIdentity]bool
	readers   []*applyReader
}

var fixturePrivacyStores sync.Map

func fixtureCanonicalPrivacy(t *testing.T, store *Store, reader memory.RecallCanonicalReader) *fixturePrivacy {
	t.Helper()

	created := &fixturePrivacy{revisions: make(map[fixturePrivacyIdentity]memory.MemoryRevision),
		sources: make(map[fixturePrivacyIdentity]bool)}

	value, loaded := fixturePrivacyStores.LoadOrStore(store, created)
	if !loaded {
		t.Cleanup(func() { fixturePrivacyStores.Delete(store) })
	}

	privacy := value.(*fixturePrivacy)
	if reader, ok := reader.(*applyReader); ok && reader != nil {
		privacy.mu.Lock()
		privacy.readers = append(privacy.readers, reader)
		privacy.mu.Unlock()
	}

	return privacy
}

func (privacy *fixturePrivacy) IsRevisionDenied(ctx context.Context, scope memory.Scope, id string) (bool, error) {
	privacy.mu.Lock()
	defer privacy.mu.Unlock()

	privacy.importReadersLocked(id)

	if _, known := privacy.revisions[fixturePrivacyIdentity{scope, id}]; !known {
		return false, memory.PermanentError(memory.CodeNotFound, "unknown synthetic revision", nil)
	}

	return false, ctx.Err()
}

func (privacy *fixturePrivacy) IsSourceDenied(ctx context.Context, scope memory.Scope, id string) (bool, error) {
	privacy.mu.Lock()
	defer privacy.mu.Unlock()

	if !privacy.sources[fixturePrivacyIdentity{scope, id}] {
		return false, memory.PermanentError(memory.CodeNotFound, "unknown synthetic source", nil)
	}

	return false, ctx.Err()
}

func (privacy *fixturePrivacy) LoadCanonicalRevisions(ctx context.Context,
	request memory.CanonicalRevisionReadRequest) ([]memory.MemoryRevision, error) {
	privacy.mu.Lock()
	defer privacy.mu.Unlock()

	var result []memory.MemoryRevision

	for _, id := range request.RevisionIDs {
		privacy.importReadersLocked(id)

		revision, known := privacy.revisions[fixturePrivacyIdentity{request.Scope, id}]
		if !known {
			return nil, memory.PermanentError(memory.CodeNotFound, "unknown synthetic structure", nil)
		}

		result = append(result, revision)
	}

	return result, ctx.Err()
}

func (privacy *fixturePrivacy) register(scope memory.Scope, id string, sources []string) {
	privacy.mu.Lock()
	defer privacy.mu.Unlock()

	privacy.registerLocked(scope, id, sources)
}

func (privacy *fixturePrivacy) registerLocked(scope memory.Scope, id string, sources []string) {
	revision := memory.MemoryRevision{RevisionID: id, ItemID: id, Revision: 1}

	for _, source := range sources {
		privacy.sources[fixturePrivacyIdentity{scope, source}] = true
		revision.Evidence = append(revision.Evidence, memory.EvidenceRef{SourceID: source})
	}

	privacy.revisions[fixturePrivacyIdentity{scope, id}] = revision
}

func (privacy *fixturePrivacy) importReadersLocked(id string) {
	for _, reader := range privacy.readers {
		if record, known := reader.records[id]; known {
			privacy.registerLocked(record.Scope, id, record.SourceIDs)
		}
	}
}

func registerSemanticPrivacy(t *testing.T, store *Store, generation semantic.Generation,
	coverage []semantic.CoverageRecord, chunks []semantic.ProjectionChunk) {
	t.Helper()

	privacy := fixtureCanonicalPrivacy(t, store, nil)
	for _, record := range coverage {
		privacy.register(generation.Scope, record.RevisionID, nil)
	}

	for _, chunk := range chunks {
		privacy.register(generation.Scope, chunk.RevisionID, chunk.SourceIDs)
	}

	if err := store.bindCanonicalPrivacy(privacy); err != nil {
		t.Fatal(err)
	}
}

func registerLexicalPrivacy(t *testing.T, store *Store, scope memory.Scope, fixtures []lexicalFixture) {
	t.Helper()

	privacy := fixtureCanonicalPrivacy(t, store, nil)
	for _, fixture := range fixtures {
		privacy.register(scope, fixture.id, fixture.sources)
	}
}
