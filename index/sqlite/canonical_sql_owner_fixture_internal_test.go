package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	"github.com/baldaworks/boyan/pkg/memory"
)

type denialCanonicalOwner interface {
	memory.CanonicalStore
	canonicalPrivacyReader
	DenySource(ctx context.Context, scope memory.Scope, id string, at time.Time) error
	DenyRevision(ctx context.Context, scope memory.Scope, id string, at time.Time) error
}

type denialOwnerFactory func(*testing.T) denialCanonicalOwner

// Keep the historical fixture until retirement; execute the identical behavior
// against each real SQL authority, allocating a fresh store for every leaf.
func runDenialAuthorities(t *testing.T, run func(*testing.T, denialOwnerFactory)) {
	t.Helper()
	t.Run("badger", func(t *testing.T) { run(t, denialCanonicalStore) })

	for _, backend := range []canonicalsqltest.Backend{canonicalsqltest.SQLite, canonicalsqltest.Postgres} {
		t.Run(string(backend), func(t *testing.T) {
			run(t, func(t *testing.T) denialCanonicalOwner {
				t.Helper()
				fixture := storefixture.New(t, backend)

				owner := fixture.Store.(interface {
					denialCanonicalOwner
					PrepareSourcePrivacy(ctx context.Context) error
				})
				if err := owner.PrepareSourcePrivacy(t.Context()); err != nil {
					t.Fatal(err)
				}

				return owner
			})
		})
	}
}
