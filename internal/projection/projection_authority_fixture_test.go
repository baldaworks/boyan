//go:build integration

package projection //nolint:testpackage // Shared native authority factories preserve the original behavior matrices.

import (
	"context"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	"github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/postgres"
	"github.com/baldaworks/boyan/store/sqlite"
)

type projectionTestOwner interface {
	projectionSQLOwner
	WalkProjectionManifests(ctx context.Context, projectionID string,
		visit func(memory.ProjectionManifest, bool) error) error
	Close() error
}

type projectionOwnerFactory func(*testing.T) (projectionTestOwner, memory.RecallCanonicalReader)

func runProjectionAuthorities(t *testing.T, run func(*testing.T, projectionOwnerFactory)) {
	t.Helper()

	for _, backend := range []canonicalsqltest.Backend{canonicalsqltest.SQLite, canonicalsqltest.Postgres} {
		t.Run(string(backend), func(t *testing.T) {
			run(t, func(t *testing.T) (projectionTestOwner, memory.RecallCanonicalReader) {
				t.Helper()
				fixture := storefixture.New(t, backend)
				store := fixture.Store.(projectionTestOwner)
				requireNoError(t, store.PrepareSourcePrivacy(t.Context()), "%v")

				var reader memory.RecallCanonicalReader

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
