package sqlitegate_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	"github.com/baldaworks/boyan/internal/forgetops"
	m "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/postgres"
	"github.com/baldaworks/boyan/store/sqlite"
)

type matchedCanonicalOwner interface {
	m.CanonicalStore
	forgetops.CanonicalForgetStore
	m.ProjectionCheckpointStore
	LoadCanonicalRevisions(ctx context.Context, request m.CanonicalRevisionReadRequest) ([]m.MemoryRevision, error)
	IsSourceDenied(ctx context.Context, scope m.Scope, id string) (bool, error)
	IsRevisionDenied(ctx context.Context, scope m.Scope, id string) (bool, error)
	DenyRevision(ctx context.Context, scope m.Scope, id string, at time.Time) error
	Close() error
}

type matchedCanonicalFactory func(context.Context, string) (matchedCanonicalOwner, m.RecallCanonicalReader, error)

func openMatchedCanonical(ctx context.Context, dir string, factories []matchedCanonicalFactory) (
	matchedCanonicalOwner, m.RecallCanonicalReader, error,
) {
	if len(factories) != 0 {
		return factories[0](ctx, dir)
	}

	store, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(dir, "canonical.sqlite")})
	if err != nil {
		return nil, nil, err
	}

	return store, sqlite.NewCanonicalReader(store), nil
}

// Correctness tests use each native owner. Live benchmarks default to SQLite;
// retired Badger benchmark sources/results remain immutable historical evidence
// in testdata/provenance, not a baseline for the changed authority.
func matchedSQLFixture(t *testing.T, name string, fixture *storefixture.Fixture) (*matchedCorpus, matchedBackend) {
	t.Helper()

	owner := fixture.Store.(interface {
		matchedCanonicalOwner
		PrepareSourcePrivacy(ctx context.Context) error
	})
	if err := owner.PrepareSourcePrivacy(t.Context()); err != nil {
		t.Fatal(err)
	}

	reader := matchedSQLReader(t, fixture.Store)

	count := 9
	if name == "semantic8192" {
		count = 8065
	}

	corpus, err := seedMatchedCorpus(t.Context(), name, count,
		func(context.Context, string) (matchedCanonicalOwner, m.RecallCanonicalReader, error) {
			return owner, reader, nil
		})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.RemoveAll(corpus.root) })

	backend, err := openMatchedBackend(t.Context(), corpus)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := backend.close(); err != nil {
			t.Error(err)
		}
	})

	if err := backend.rebuild(t.Context(), "baseline-v1"); err != nil {
		t.Fatal(err)
	}

	if name == "semantic8192" {
		if err := backend.semantic(t.Context(), "ready"); err != nil {
			t.Fatal(err)
		}
	}

	return corpus, backend
}

func matchedSQLReader(t *testing.T, owner storefixture.Store) m.RecallCanonicalReader {
	t.Helper()

	switch store := owner.(type) {
	case *sqlite.Store:
		return sqlite.NewCanonicalReader(store)
	case *postgres.Store:
		return postgres.NewCanonicalReader(store)
	default:
		t.Fatalf("unexpected matched fixture owner %T", store)

		return nil
	}
}
