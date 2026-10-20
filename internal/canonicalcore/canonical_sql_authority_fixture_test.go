package canonicalcore_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalops"
	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	"github.com/baldaworks/boyan/internal/forgetops"
	m "github.com/baldaworks/boyan/pkg/memory"
	b "github.com/baldaworks/boyan/store/badger"
	"github.com/baldaworks/boyan/store/postgres"
	"github.com/baldaworks/boyan/store/sqlite"
)

type coreOwner interface {
	coreMutationOwner
	corePrivacyOwner
	m.ProjectionCheckpointStore
	m.ScopeCheckpointStore
	forgetops.CanonicalForgetStore
	Close() error
}

type coreMutationOwner interface {
	m.CanonicalStore
	m.CanonicalOperationReader
	m.CanonicalOperationCommitter
	m.CanonicalTurnIdentityStore
	m.CanonicalBoundaryIdentityStore
	m.IngestSequenceStore
	m.CanonicalForgetOutcomeStore
	m.CanonicalSourceForgetStore
	m.CanonicalForgetEnumerator
}

type corePrivacyOwner interface {
	PrepareSourcePrivacy(ctx context.Context) error
	LoadPayload(ctx context.Context, ref m.PayloadRef) ([]byte, error)
	DenySource(ctx context.Context, scope m.Scope, id string, at time.Time) error
	DenyRevision(ctx context.Context, scope m.Scope, id string, at time.Time) error
	IsSourceDenied(ctx context.Context, scope m.Scope, id string) (bool, error)
	IsRevisionDenied(ctx context.Context, scope m.Scope, id string) (bool, error)
}

type coreReader interface {
	m.RecallCanonicalReader
	m.CanonicalDerivedReader
	canonicalops.BoundaryCanonicalReader
	LoadCanonicalBoundaryViewAt(ctx context.Context, scope m.Scope, at time.Time) (m.CanonicalBoundaryView, error)
}

// A nil factory selects the retained Badger constructor in each old fixture.
// SQL factories cache only reopen constructors, never substitute an authority.
type coreFactory func(*testing.T, string) (coreOwner, coreReader)

var coreDatabases sync.Map

func openCoreAuthority(t *testing.T, path string, factory coreFactory) (coreOwner, coreReader) {
	t.Helper()

	if factory != nil {
		return factory(t, path)
	}

	store, err := b.OpenCanonicalStore(path)
	requireNoError(t, err, "open retained Badger authority: %v")

	return store, readerForCoreOwner(t, store)
}

func readerForCoreOwner(t *testing.T, store coreOwner) coreReader {
	t.Helper()

	switch owner := store.(type) {
	case *b.CanonicalStore:
		reader, err := b.NewCanonicalReader(owner)
		requireNoError(t, err, "reader: %v")

		return reader
	case *sqlite.Store:
		return sqlite.NewCanonicalReader(owner)
	case *postgres.Store:
		return postgres.NewCanonicalReader(owner)
	default:
		t.Fatalf("unexpected canonical authority %T", store)

		return nil
	}
}

func runCoreAuthorities(t *testing.T, run func(*testing.T, coreFactory)) {
	t.Helper()
	t.Run("badger", func(t *testing.T) { run(t, nil) })

	for _, backend := range []canonicalsqltest.Backend{canonicalsqltest.SQLite, canonicalsqltest.Postgres} {
		t.Run(string(backend), func(t *testing.T) {
			var mu sync.Mutex

			reopens := make(map[string]func(context.Context) (storefixture.Store, error))
			databases := make(map[string]*canonicalsqltest.Database)

			run(t, func(t *testing.T, path string) (coreOwner, coreReader) {
				t.Helper()
				mu.Lock()
				defer mu.Unlock()

				var store storefixture.Store

				fresh := false

				if open, found := reopens[path]; found {
					var err error

					store, err = open(t.Context())
					requireNoError(t, err, "reopen native authority: %v")
				} else {
					fresh = true
					fixture := storefixture.New(t, backend)
					store = fixture.Store
					reopens[path] = fixture.Open
					databases[path] = fixture.Database
				}

				owner := store.(coreOwner)
				if fresh {
					requireNoError(t, owner.PrepareSourcePrivacy(t.Context()), "prepare fresh native authority: %v")
				}

				coreDatabases.Store(owner, databases[path])
				t.Cleanup(func() { coreDatabases.Delete(owner) })

				var reader coreReader

				switch concrete := store.(type) {
				case *sqlite.Store:
					reader = sqlite.NewCanonicalReader(concrete)
				case *postgres.Store:
					reader = postgres.NewCanonicalReader(concrete)
				}

				return owner, reader
			})
		})
	}
}
