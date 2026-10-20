package storefixture_test

import (
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/postgres"
)

// SQLite's original PKs prohibit duplicate exact identities; these physical
// corruption cases exercise PostgreSQL's serialized exact original-frame resolver.
func TestPublicPostgresMutationRejectsDuplicateExactIdentity(t *testing.T) {
	for _, kind := range []string{"row"} {
		t.Run(kind, func(t *testing.T) {
			db := canonicalsqltest.PostgresDatabase(t)

			store, err := postgres.Open(t.Context(), postgres.Config{DSN: db.DSN, AllowLocalInsecure: true})
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()

			if err := store.PrepareSourcePrivacy(t.Context()); err != nil {
				t.Fatal(err)
			}

			probe := newMutationProbe(t, &storefixture.Fixture{Database: db})

			mutation := nativeMutation(t)
			if _, err := store.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
				t.Fatal(err)
			}

			statement := "INSERT INTO boyan_canonical.scope_state " +
				"(logical_key,scope_partition,scope_key,scope_kind,envelope,version_u64,change_seq_u64) " +
				"SELECT logical_key,scope_partition,scope_key,scope_kind,envelope,version_u64,change_seq_u64 " +
				"FROM boyan_canonical.scope_state"
			if err := probe.exec(statement); err != nil {
				t.Fatal(err)
			}

			_, err = store.ApplyCanonicalMutation(t.Context(), laterMutation(mutation, "must-not-commit", 1))
			assertMutationCode(t, err, memory.CodeStoreFailure)

			_, found, err := store.LoadCanonicalOperation(t.Context(), mutation.Scope, "must-not-commit")
			if err != nil || found {
				t.Fatalf("partial operation=%v %v", found, err)
			}
		})
	}
}
