//go:build integration

package sqlite

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/baldaworks/boyan/pkg/memory"
)

type privacyTestAuthority struct {
	mu             sync.Mutex
	scope          memory.Scope
	revisions      map[string]memory.MemoryRevision
	sources        map[string]bool
	denied         map[string]bool
	err            error
	wrong, missing bool
	before         func(context.Context)
	loads          int
}

func newPrivacyTestAuthority(scope memory.Scope, ids ...string) *privacyTestAuthority {
	p := &privacyTestAuthority{scope: scope, revisions: make(map[string]memory.MemoryRevision),
		sources: map[string]bool{"source": false, "evidence": false, "membership": false}, denied: make(map[string]bool)}
	for _, id := range ids {
		p.revisions[id] = memory.MemoryRevision{RevisionID: id, Evidence: []memory.EvidenceRef{{SourceID: "evidence"}}}
	}

	return p
}
func (p *privacyTestAuthority) IsRevisionDenied(ctx context.Context, scope memory.Scope, id string) (bool, error) {
	if p.before != nil {
		p.before(ctx)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	if p.err != nil {
		return false, p.err
	}

	if _, known := p.revisions[id]; scope != p.scope || !known {
		return false, memory.PermanentError(memory.CodeNotFound, "unknown test revision", nil)
	}

	return p.denied[id], nil
}
func (p *privacyTestAuthority) IsSourceDenied(ctx context.Context, scope memory.Scope, id string) (bool, error) {
	if p.before != nil {
		p.before(ctx)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	denied, known := p.sources[id]
	if scope != p.scope || !known {
		return false, memory.PermanentError(memory.CodeNotFound, "unknown test source", nil)
	}

	return denied, ctx.Err()
}
func (p *privacyTestAuthority) LoadCanonicalRevisions(ctx context.Context,
	request memory.CanonicalRevisionReadRequest) ([]memory.MemoryRevision, error) {
	if p.before != nil {
		p.before(ctx)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.loads++
	if request.Scope != p.scope || len(request.RevisionIDs) != 1 {
		return nil, errors.New("non-single or wrong scope lookup")
	}

	if p.missing {
		return nil, nil
	}

	r, known := p.revisions[request.RevisionIDs[0]]
	if !known {
		return nil, memory.PermanentError(memory.CodeNotFound, "missing test structure", nil)
	}

	if p.wrong {
		r.RevisionID = "wrong"
	}

	return []memory.MemoryRevision{r}, ctx.Err()
}
func (p *privacyTestAuthority) denyRevision(id string) {
	p.mu.Lock()
	p.denied[id] = true
	p.mu.Unlock()
}

type noncomparablePrivacy struct {
	*privacyTestAuthority

	values []string
}

type nestedNoncomparablePrivacy struct {
	canonicalPrivacyReader

	identity any
}

//nolint:cyclop // Constructor failures and successful ownership preservation are independently asserted.
func TestCanonicalPrivacyBinding(t *testing.T) {
	store, _ := openTestStore(t, false)
	reader := &applyReader{}
	authority := newPrivacyTestAuthority(lexicalTestScope)

	assertInvalidPrivacyConstruction(t, store, reader, authority)

	first, err := NewSQLiteCanonicalApplier(store, reader, authority)
	if err != nil {
		t.Fatal(err)
	}

	second, err := NewSQLiteCanonicalApplier(store, reader, authority)
	if err != nil {
		t.Fatal(err)
	}

	other := newPrivacyTestAuthority(lexicalTestScope)
	if a, err := NewSQLiteCanonicalApplier(store, reader, other); a != nil || !privacyCode(err, memory.CodeConflict) {
		t.Fatalf("authority replaced: %v %v", a, err)
	}

	invalid, err := NewSQLiteCanonicalApplier(store, (*applyReader)(nil), other)
	if invalid != nil || !privacyCode(err, memory.CodeStoreFailure) {
		t.Fatalf("failed reader validation replaced authority: %v %v", invalid, err)
	}

	bound, err := store.canonicalPrivacy()
	if err != nil || bound != authority {
		t.Fatalf("original authority lost: %v %v", bound, err)
	}

	if err := first.BeginProjectionRebuild(t.Context(), lexicalTestScope, "empty"); err != nil {
		t.Fatal(err)
	}

	if err := second.BeginProjectionRebuild(t.Context(), lexicalTestScope, "empty"); !privacyRetryableConflict(err) {
		t.Fatalf("competing applier adopted stage: %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	if a, err := NewSQLiteCanonicalApplier(store, reader, authority); a != nil || !errors.Is(err, ErrClosed) {
		t.Fatalf("closed store bound: %v %v", a, err)
	}
}
func privacyCode(err error, code memory.ErrorCode) bool {
	var classified *memory.Error

	return errors.As(err, &classified) && classified.Code == code && !memory.IsRetryable(err)
}
func privacyRetryableConflict(err error) bool {
	var classified *memory.Error

	return errors.As(err, &classified) && classified.Code == memory.CodeConflict && memory.IsRetryable(err)
}

func TestCanonicalPrivacyUnboundMaterialWrite(t *testing.T) {
	for _, entry := range []string{"apply", "begin", "end", "lazy copy", "stage"} {
		t.Run(entry, func(t *testing.T) {
			store, _ := openTestStore(t, true)
			a := &SQLiteCanonicalApplier{store: store, reader: &applyReader{}, stages: make(map[memory.Scope]*lexicalStage)}

			var err error

			switch entry {
			case "apply":
				err = a.Apply(t.Context(), lexicalTestScope, "new", nil, nil)
			case "begin":
				err = a.BeginProjectionRebuild(t.Context(), lexicalTestScope, "new")
			case "end":
				err = a.EndProjectionRebuild(t.Context(), lexicalTestScope, "new")
			case "lazy copy":
				err = a.CommitProjectionManifest(t.Context(), publicationManifest("new", 1))
			case "stage":
				g, r, c := semanticFixture()
				_, err = store.Stage(t.Context(), g, r, c)
			}

			if !privacyCode(err, memory.CodeStoreFailure) {
				t.Fatalf("unbound admission: %v", err)
			}

			for _, table := range []string{
				"lexical_build", "lexical_record", "semantic_build", "semantic_coverage", "semantic_chunk",
			} {
				assertPrivacyCount(t, store, "SELECT count(*) FROM "+table, 0)
			}
		})
	}
}

func assertInvalidPrivacyConstruction(t *testing.T, store *Store,
	reader *applyReader, authority *privacyTestAuthority) {
	t.Helper()

	var (
		typedReader    *applyReader
		typedAuthority *privacyTestAuthority
	)
	for _, test := range []struct {
		name    string
		store   *Store
		reader  memory.RecallCanonicalReader
		privacy canonicalPrivacyReader
	}{
		{"nil store", nil, reader, authority},
		{"nil reader", store, nil, authority},
		{"typed nil reader", store, typedReader, authority},
		{"nil authority", store, reader, nil},
		{"typed nil authority", store, reader, typedAuthority},
		{"noncomparable authority", store, reader, noncomparablePrivacy{privacyTestAuthority: authority}},
		{"nested noncomparable identity", store, reader, nestedNoncomparablePrivacy{
			canonicalPrivacyReader: authority, identity: []string{"not comparable"},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, err := NewSQLiteCanonicalApplier(test.store, test.reader, test.privacy)
			if a != nil || !privacyCode(err, memory.CodeStoreFailure) {
				t.Fatalf("invalid binding produced usable applier: %v %v", a, err)
			}

			if _, err := store.canonicalPrivacy(); err == nil {
				t.Fatal("failed construction bound store")
			}
		})
	}
}
