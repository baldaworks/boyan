package canonicalsql

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func sourceCertifiedOperation(t *testing.T, owner *Owner, scope memory.Scope, id string, version uint64) {
	t.Helper()

	if err := owner.PrepareSourcePrivacy(t.Context()); err != nil {
		t.Fatal(err)
	}

	identity := proofBoundary(t, scope)

	_, err := owner.CommitCanonicalOperation(t.Context(), memory.CanonicalOperationCommitRequest{
		Scope: scope, ExpectedScopeVersion: version, OperationID: id, Fingerprint: id,
		CommittedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), BoundaryIdentity: &identity,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func requireSourceCapability(t *testing.T, owner *Owner, scope memory.Scope, want bool) {
	t.Helper()

	err := owner.connection.run(t.Context(), false, false, func(tx *Tx) error {
		chain, err := tx.readRestoreControl(t.Context(), "restore_chain")
		if err != nil {
			return err
		}

		if chain == nil || chain.Version != 2 || chain.Phase != "complete" ||
			!validRestoreGeneration(chain.ActiveGeneration) {
			t.Fatal("restore did not publish complete source generation")
		}

		found, err := tx.hasRestoreSource(t.Context(), chain.ActiveGeneration, scope)
		if err == nil && found != want {
			t.Fatalf("source evidence = %v, want %v", found, want)
		}

		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

//nolint:cyclop,funlen // The two-increment chain, local writes, and source tombstone share one durable lineage.
func TestRestoreSourceOmittedMarkersAndTombstone(t *testing.T) {
	forEachOwner(t, func(t *testing.T, source *Owner, _ func() *Owner) {
		scope := testScope()
		sourceCertifiedOperation(t, source, scope, legacyDerivedOperationPrefix+strings.Repeat("a", 64), 0)
		full, base := sourceBackup(t, source, 0)

		destination, reopen := restoreSourceDestination(t, source)
		if err := destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(full), 1); err != nil {
			t.Fatal(err)
		}

		for n := uint64(1); n <= 2; n++ {
			if err := destination.Close(); err != nil {
				t.Fatal(err)
			}

			destination = reopen()
			sourceMarker(t, destination, memory.Scope{Key: "test:local-only", Kind: scope.Kind})
			sourceCertifiedOperation(t, source, scope, legacyDerivedOperationPrefix+fmt.Sprintf("%064d", n), n)

			delta, end := sourceBackup(t, source, base)
			if bytes.Contains(delta, []byte(`"family":"boundary-capability"`)) {
				t.Fatal("fixture did not omit unchanged marker")
			}

			if err := destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(delta), 1); err != nil {
				t.Fatal(err)
			}

			requireSourceCapability(t, destination, scope, true)
			requireSourceCapability(t, destination, memory.Scope{Key: "test:local-only", Kind: scope.Kind}, false)

			base = end
		}

		if err := source.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
			key, err := privacyKey(scope, BoundaryCapability, boundaryCapabilityID)
			if err != nil {
				return err
			}

			return tx.Delete(ctx, key)
		}); err != nil {
			t.Fatal(err)
		}

		delta, base := sourceBackup(t, source, base)
		if err := destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(delta), 1); err != nil {
			t.Fatal(err)
		}

		requireSourceCapability(t, destination, scope, false)

		if err := destination.View(t.Context(), func(ctx context.Context, tx *Tx) error {
			found, err := loadBoundaryCapability(ctx, tx, scope)
			if err == nil && !found {
				t.Fatal("source tombstone erased destination restriction")
			}

			return err
		}); err != nil {
			t.Fatal(err)
		}

		sourceOperation(t, source, scope, legacyDerivedOperationPrefix+strings.Repeat("b", 64), 3)

		delta, _ = sourceBackup(t, source, base)
		if err := destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(delta), 1); err == nil {
			t.Fatal("source tombstone retained positive authority")
		}
	})
}

// Same raw scope key with another kind, and another key with the same kind,
// cannot borrow source evidence. An identical legacy record cannot be upgraded.
//
//nolint:cyclop,gocognit,funlen // Incompatible source/destination combinations share reopen/retry assertions.
func TestRestoreSourceScopeIsolationAndLegacyRestriction(t *testing.T) {
	for _, kind := range []string{"other-kind", "other-key", "legacy-destination", "unproven-full", "unproven-logical"} {
		t.Run(kind, func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, source *Owner, _ func() *Owner) {
				scope := testScope()

				other := scope
				if kind == "other-kind" {
					other.Kind = memory.ScopeKindGroup
				} else if kind == "other-key" {
					other.Key += ":other"
				}

				id := legacyDerivedOperationPrefix + strings.Repeat("a", 64)

				destination, reopen := restoreSourceDestination(t, source)
				if kind == "legacy-destination" {
					sourceOperation(t, destination, scope, id, 0)
					sourceCertifiedOperation(t, source, scope, id, 0)
				} else {
					sourceMarker(t, destination, other)

					if strings.HasPrefix(kind, "other-") {
						sourceMarker(t, source, scope)
					}

					sourceOperation(t, source, other, id, 0)
				}

				var data []byte

				if kind == "unproven-logical" {
					var out bytes.Buffer
					if err := source.ExportCanonicalLogical(t.Context(), &out); err != nil {
						t.Fatal(err)
					}

					data = out.Bytes()
				} else {
					data, _ = sourceBackup(t, source, 0)
				}

				for round := range 2 {
					var err error
					if kind == "unproven-logical" {
						err = destination.ImportCanonicalLogical(t.Context(), bytes.NewReader(data))
					} else {
						err = destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(data), 1)
					}

					if err == nil {
						t.Fatalf("incompatible merge accepted on attempt %d", round)
					}

					if err := destination.VerifyCanonicalIntegrity(t.Context()); err == nil {
						t.Fatal("failed merge became available")
					}

					if err := destination.Close(); err != nil {
						t.Fatal(err)
					}

					destination = reopen()
				}
			})
		})
	}
}
