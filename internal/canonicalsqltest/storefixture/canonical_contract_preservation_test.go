//go:build integration

package storefixture_test

import (
	"testing"

	"github.com/baldaworks/boyan/boyantest"
	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestPublicCanonicalStoreOriginalContract(t *testing.T) {
	for _, backend := range []canonicalsqltest.Backend{canonicalsqltest.SQLite, canonicalsqltest.Postgres} {
		t.Run(string(backend), func(t *testing.T) {
			boyantest.RunCanonicalStoreContract(t, func(t *testing.T) memory.CanonicalStore {
				fixture := storefixture.New(t, backend)

				store, ok := fixture.Store.(memory.CanonicalStore)
				if !ok {
					t.Fatal("native owner lacks canonical contract")
				}

				prepare, ok := fixture.Store.(mutationStore)
				if !ok {
					t.Fatal("native owner lacks privacy preparation")
				}

				requireProjectionNoError(t, prepare.PrepareSourcePrivacy(t.Context()))

				return store
			})
		})
	}
}
