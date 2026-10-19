package memoryread_test

import (
	"context"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	m "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/postgres"
	"github.com/baldaworks/boyan/store/sqlite"
)

type recallOwner interface {
	m.CanonicalStore
	m.ProjectionCheckpointStore
	IsSourceDenied(ctx context.Context, scope m.Scope, id string) (bool, error)
	IsRevisionDenied(ctx context.Context, scope m.Scope, id string) (bool, error)
	DenySource(ctx context.Context, scope m.Scope, id string, at time.Time) error
	RunScopeErase(ctx context.Context, request m.ScopeEraseRequest) (m.ScopeEraseJournal, error)
	Close() error
}

type recallOwnerFactory func(*testing.T) (recallOwner, m.RecallCanonicalReader)

func runRecallAuthorities(t *testing.T, run func(*testing.T, recallOwnerFactory)) {
	t.Helper()
	t.Run("badger", func(t *testing.T) { run(t, nil) })

	for _, backend := range []canonicalsqltest.Backend{canonicalsqltest.SQLite, canonicalsqltest.Postgres} {
		t.Run(string(backend), func(t *testing.T) {
			run(t, func(t *testing.T) (recallOwner, m.RecallCanonicalReader) {
				t.Helper()
				fixture := storefixture.New(t, backend)

				store := fixture.Store.(interface {
					recallOwner
					PrepareSourcePrivacy(ctx context.Context) error
				})
				if err := store.PrepareSourcePrivacy(t.Context()); err != nil {
					t.Fatal(err)
				}

				var reader m.RecallCanonicalReader

				switch owner := fixture.Store.(type) {
				case *sqlite.Store:
					reader = sqlite.NewCanonicalReader(owner)
				case *postgres.Store:
					reader = postgres.NewCanonicalReader(owner)
				}

				return store, reader
			})
		})
	}
}
