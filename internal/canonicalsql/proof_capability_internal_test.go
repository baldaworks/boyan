package canonicalsql

import (
	"context"
	"strconv"
	"strings"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestProofCapabilitySelectiveHistory(t *testing.T) {
	forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
		scope := testScope()

		requireProofSuccess(t, owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
			for i := range 1024 {
				key, _ := privacyKey(scope, Operation, "native:"+strconv.Itoa(i))
				if err := tx.Put(ctx, key, envelope(t, Operation, true)); err != nil {
					return err
				}
			}

			return nil
		}))

		assertProofClassification(t, owner, scope, false)

		for _, suffix := range []string{strings.Repeat("0", 64), strings.Repeat("!", 64)} {
			id := legacyDerivedOperationPrefix + suffix

			requireProofSuccess(t, owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
				key, _ := privacyKey(scope, Operation, id)

				return tx.Put(ctx, key, envelope(t, Operation, true))
			}))

			rawExec(t,
				owner.connection,
				"UPDATE "+owner.connection.table("operation")+" SET envelope=$1 WHERE id=$2",
				[]byte("opaque"),
				[]byte(id))
			assertProofClassification(t, owner, scope, true)
			rawExec(t,
				owner.connection,
				"UPDATE "+owner.connection.table("operation")+" SET envelope=$1 WHERE id=$2",
				envelope(t,
					Operation,
					true),
				[]byte(id))

			requireProofSuccess(t, owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
				key, _ := privacyKey(scope, Operation, id)

				return tx.Delete(ctx, key)
			}))
		}
	})
}

func assertProofClassification(t *testing.T, owner *Owner, scope memory.Scope, conflict bool) {
	t.Helper()

	err := owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
		recorder := &hydrationQuery{base: tx.q}
		tx.q = recorder

		result, err := classifyBoundaryScope(ctx, tx, scope)
		if conflict {
			assertCode(t, err, memory.CodeConflict)
		} else if err != nil || !result.SafelyNew {
			t.Fatalf("classification: %+v %v", result, err)
		}

		for _, statement := range recorder.statements {
			if strings.Contains(statement,
				"operation") &&
				(strings.Contains(statement,
					"ORDER BY") ||
					strings.Contains(statement,
						"envelope")) {
				t.Fatal("classification sorted/hydrated operation history")
			}
		}

		return nil
	})
	requireProofSuccess(t, err)
}

func TestProofCapabilityExactNamespace(t *testing.T) {
	for _, kind := range []string{"short",
		"long",
		"different-prefix",
		"other-kind",
		"other-key",
		"marked",
		"old-operation",
		"long-scope"} {
		t.Run(kind, func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
				scope := testScope()
				if kind == "long-scope" {
					scope.Key = "test:" + strings.Repeat("q", 65530)
				}

				identity := proofBoundary(t, scope)
				id := legacyDerivedOperationPrefix + strings.Repeat("!", 64)
				storedScope := scope

				storedScope, id = proofNamespaceVariant(storedScope, id, kind)

				err := seedProofCapability(t, owner, identity, storedScope, id, kind)

				if kind == "marked" {
					assertCode(t, err, memory.CodeConflict)

					return
				}

				requireProofSuccess(t, err)

				if kind == "old-operation" {
					err = owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
						key, _ := privacyKey(scope, Operation, id)

						return prepareBoundaryIdentity(ctx, tx, scope, &identity, id, key)
					})
					assertCode(t, err, memory.CodeConflict)

					return
				}

				result, err := owner.VerifyCanonicalBoundary(t.Context(), identity.Boundary)
				if err != nil || (kind == "long-scope" && !result.Certified) || (kind != "long-scope" && !result.SafelyNew) {
					t.Fatalf("namespace: %+v %v", result, err)
				}
			})
		})
	}
}

func proofNamespaceVariant(scope memory.Scope, id, kind string) (memory.Scope, string) {
	switch kind {
	case "short":
		id = id[:len(id)-1]
	case "long":
		id += "x"
	case "different-prefix":
		id = "other:" + id
	case "other-kind":
		scope.Kind = memory.ScopeKindGroup
	case "other-key":
		scope.Key = "test:other"
	}

	return scope, id
}

func seedProofCapability(t *testing.T, owner *Owner, identity memory.CanonicalBoundaryIdentityCommit,
	storedScope memory.Scope, id, kind string) error {
	t.Helper()

	return owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		if kind == "long-scope" {
			return persistBoundaryCertificate(ctx, tx, identity.Boundary.Scope, &identity)
		}

		if kind == "marked" {
			if err := persistBoundaryCapability(ctx, tx, identity.Boundary.Scope); err != nil {
				return err
			}

			return rejectUnprovenDerivedOperation(ctx, tx, identity.Boundary.Scope, id)
		}

		key, _ := privacyKey(storedScope, Operation, id)

		return tx.Put(ctx, key, envelope(t, Operation, true))
	})
}
