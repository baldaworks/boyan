package storefixture_test

import (
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
)

func TestRegisteredOwnerFactory(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, fixture *storefixture.Fixture) {
		if fixture.Backend != canonicalsqltest.SQLite && fixture.Backend != canonicalsqltest.Postgres {
			t.Fatal("unknown owner factory backend")
		}

		if err := fixture.Store.RestorePreflight(t.Context()); err != nil {
			t.Fatal(err)
		}

		if second, err := fixture.Open(t.Context()); err == nil || second != nil {
			t.Fatal("factory bypassed exclusive owner")
		}

		if err := fixture.Store.Close(); err != nil {
			t.Fatal(err)
		}

		reopened, err := fixture.Open(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		if err := reopened.Close(); err != nil {
			t.Fatal(err)
		}
	})
}
