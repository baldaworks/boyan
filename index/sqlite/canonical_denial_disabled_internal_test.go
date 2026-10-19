//nolint:lll // Fixed SQL assertions distinguish retained invalid-vector bytes from exact degraded epoch metadata.
package sqlite

import (
	"fmt"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/pkg/memory"
)

//nolint:cyclop // Check complete persisted scope retirement and independent ownership-scan counts together.
func TestCanonicalDenialReconcileAllPersistedScopes(t *testing.T) {
	runDenialAuthorities(t, func(t *testing.T, newCanonical denialOwnerFactory) {
		s := newCanonical(t)
		store, dir := openTestStore(t, true)

		for i := range 131 {
			scope := memory.Scope{Kind: memory.ScopeKindGroup, Key: fmt.Sprintf("scope:%03d", i)}
			seedLexical(t, store, fmt.Sprintf("retained-%03d", i), scope, []lexicalFixture{{id: "denied", raw: "needle"}})

			if err := s.DenyRevision(t.Context(), scope, "denied", lexicalTestTime); err != nil {
				t.Fatal(err)
			}
		}
		// No lexical or active canonical scope enumeration can discover this owner.
		semanticScope := memory.Scope{Kind: memory.ScopeKindPersonal, Key: "scope:000"}
		seedDenialSemantic(t, store, semanticScope, "semantic-only", "denied", false, false)

		if err := s.DenyRevision(t.Context(), semanticScope, "denied", lexicalTestTime); err != nil {
			t.Fatal(err)
		}

		if err := store.bindCanonicalPrivacy(s); err != nil {
			t.Fatal(err)
		}

		observer := observeReader(t, store, dir)
		if err := store.ReconcileAllCanonicalDenials(t.Context()); err != nil {
			t.Fatal(err)
		}

		checks := 0

		for _, query := range observer.take() {
			if strings.Contains(query.query, "pragma_foreign_key_check") {
				checks++
			}
		}

		if checks != 1 {
			t.Errorf("startup ownership scans = %d, want one across all persisted scopes", checks)
		}

		if err := store.ReconcileCanonicalDenials(t.Context(), semanticScope); err != nil {
			t.Fatal(err)
		}

		checks = 0

		for _, query := range observer.take() {
			if strings.Contains(query.query, "pragma_foreign_key_check") {
				checks++
			}
		}

		if checks != 1 {
			t.Errorf("standalone ownership scans = %d, want its own global check", checks)
		}

		assertPrivacyCount(t, store, `SELECT count(*) FROM lexical_record`, 0)
		assertPrivacyCount(t, store, `SELECT count(*) FROM semantic_coverage`, 0)
	})
}

func TestCanonicalDenialReconcileDisabledMetadataOnly(t *testing.T) {
	runDenialAuthorities(t, func(t *testing.T, newCanonical denialOwnerFactory) {
		t.Run("fresh", func(t *testing.T) {
			s := newCanonical(t)

			store, _ := openTestStore(t, false)
			if err := store.bindCanonicalPrivacy(s); err != nil {
				t.Fatal(err)
			}

			if err := store.ReconcileAllCanonicalDenials(t.Context()); err != nil {
				t.Fatal(err)
			}

			assertPrivacyCount(t, store, `SELECT count(*) FROM sqlite_schema WHERE name LIKE 'semantic_%'`, 0)
		})
		t.Run("old invalid vectors", func(t *testing.T) {
			s := newCanonical(t)
			store, dir := openTestStore(t, true)
			denialCanonicalRevision(t, s, lexicalTestScope, "safe", "source")
			seedDenialSemantic(t, store, lexicalTestScope, "target", "denied", true, true)
			seedDenialSemantic(t, store, lexicalTestScope, "safe-peer", "safe", false, true)
			execTest(t, store.writer, `UPDATE semantic_chunk SET embedding=x'00000040'`)

			if err := store.Close(); err != nil {
				t.Fatal(err)
			}

			reopened, err := Open(t.Context(), dir, Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = reopened.Close() }()

			if err := reopened.bindCanonicalPrivacy(s); err != nil {
				t.Fatal(err)
			}

			if err := s.DenyRevision(t.Context(), lexicalTestScope, "denied", lexicalTestTime); err != nil {
				t.Fatal(err)
			}

			if err := reopened.ReconcileAllCanonicalDenials(t.Context()); err != nil {
				t.Fatal(err)
			}

			assertPrivacyCount(t, reopened, `SELECT count(*) FROM semantic_chunk WHERE build_id='target'`, 0)
			assertPrivacyCount(t, reopened, `SELECT count(*) FROM semantic_chunk WHERE build_id='safe-peer' AND embedding=x'00000040'`, 1)
			assertPrivacyCount(t, reopened, `SELECT count(*) FROM semantic_build WHERE build_id='target' AND complete=1 AND status='degraded' AND epoch=?`, 1, encodeUint64(7))
		})
	})
}
