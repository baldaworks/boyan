package canonicalsql

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func restoreSourceDestination(t *testing.T, source *Owner) (*Owner, func() *Owner) {
	t.Helper()

	if source.connection.pg == nil {
		path := filepath.Join(t.TempDir(), "destination.sqlite")
		open := func() *Owner { return openTestOwner(t, path) }

		return open(), open
	}

	db := canonicalsqltest.PostgresDatabase(t)
	open := func() *Owner {
		return openPostgresTestOwner(t, PostgresConfig{DSN: db.DSN, AllowLocalInsecure: true})
	}

	return open(), open
}

func sourceBackup(t *testing.T, source *Owner, since uint64) ([]byte, uint64) {
	t.Helper()

	var out bytes.Buffer

	end, err := source.WriteCanonicalBackup(t.Context(), &out, since)
	if err != nil {
		t.Fatal(err)
	}

	return out.Bytes(), end
}

func sourceMarker(t *testing.T, owner *Owner, scope memory.Scope) {
	t.Helper()

	if err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		return persistBoundaryCapability(ctx, tx, scope)
	}); err != nil {
		t.Fatal(err)
	}
}

func sourceOperation(t *testing.T, owner *Owner, scope memory.Scope, id string, version uint64) {
	t.Helper()

	if err := owner.PrepareSourcePrivacy(t.Context()); err != nil {
		t.Fatal(err)
	}

	_, err := owner.CommitCanonicalOperation(t.Context(), memory.CanonicalOperationCommitRequest{
		Scope: scope, ExpectedScopeVersion: version, OperationID: id, Fingerprint: id,
		CommittedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A destination-only promise must not become evidence of omitted source markers.
func TestRestoreSourceProvenanceRejectsDestinationLaundering(t *testing.T) {
	forEachOwner(t, func(t *testing.T, source *Owner, _ func() *Owner) {
		scope := testScope()
		destination, reopen := restoreSourceDestination(t, source)
		sourceMarker(t, destination, scope)
		sourceOperation(t, source, scope, "native-base", 0)

		full, base := sourceBackup(t, source, 0)
		if err := destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(full), 1); err != nil {
			t.Fatal(err)
		}

		if err := destination.Close(); err != nil {
			t.Fatal(err)
		}

		destination = reopen()
		// A normal local write must not alter the remembered source set.
		sourceMarker(t, destination, memory.Scope{Key: scope.Key, Kind: memory.ScopeKindGroup})
		sourceOperation(t, source, scope, "native-increment", 1)

		delta, base := sourceBackup(t, source, base)
		if err := destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(delta), 1); err != nil {
			t.Fatal(err)
		}

		sourceOperation(t, source, scope, legacyDerivedOperationPrefix+strings.Repeat("a", 64), 2)

		delta, _ = sourceBackup(t, source, base)
		if err := destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(delta), 1); err == nil {
			t.Fatal("destination capability laundered new proof-free source history")
		}

		if err := destination.SourcePrivacyReady(t.Context()); err == nil {
			t.Fatal("failed merge removed loading fence")
		}
	})
}

// The marker must survive and appear in a later local backup; private evidence must not.
func TestRestoreSourceCapabilityPreserved(t *testing.T) {
	forEachOwner(t, func(t *testing.T, source *Owner, _ func() *Owner) {
		scope := testScope()
		sourceMarker(t, source, scope)
		full, _ := sourceBackup(t, source, 0)

		destination, _ := restoreSourceDestination(t, source)
		if err := destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(full), 1); err != nil {
			t.Fatal(err)
		}

		if err := destination.View(t.Context(), func(ctx context.Context, tx *Tx) error {
			found, err := loadBoundaryCapability(ctx, tx, scope)
			if err == nil && !found {
				t.Fatal("source capability discarded")
			}

			return err
		}); err != nil {
			t.Fatal(err)
		}

		local, _ := sourceBackup(t, destination, 0)
		if !bytes.Contains(local, []byte(`"family":"boundary-capability"`)) ||
			bytes.Contains(local, []byte("restore_source_capability")) {
			t.Fatal("local backup lost canonical marker or exposed private provenance")
		}
	})
}
