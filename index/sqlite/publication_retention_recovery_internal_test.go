//go:build integration

package sqlite

import (
	"errors"
	"testing"
	"time"

	"github.com/baldaworks/boyan/pkg/memory"
)

func TestPublicationIncompleteRetainsCandidatesUntilRecovery(t *testing.T) {
	store, _ := openTestStore(t, false)
	c := &publicationCheckpoints{active: publicationManifest("current", 3)}
	a, lifecycle := publicationLifecycle(t, store, &applyReader{}, c)

	for i, generation := range []string{"oldest", "previous"} {
		if err := a.BeginProjectionRebuild(t.Context(), lexicalTestScope, generation); err != nil {
			t.Fatal(err)
		}

		manifest := publicationManifest(generation, uint64(i+1))

		manifest.UpdatedAt = lexicalTestTime.Add(-time.Duration(i) * time.Second)
		if err := lifecycle.CommitProjectionManifest(t.Context(), manifest); err != nil {
			t.Fatal(err)
		}

		manifest.Status = memory.ProjectionGenerationSuperseded
		c.previous = append(c.previous, manifest)

		if err := lifecycle.AbortProjectionSync(t.Context(), generation); err != nil {
			t.Fatal(err)
		}
	}

	if err := lifecycle.ReconcileProjection(t.Context()); err != nil {
		t.Fatal(err)
	}

	assertPrivacyCount(t, store, "SELECT count(*) FROM lexical_build", 2)

	if err := a.BeginProjectionRebuild(t.Context(), lexicalTestScope, "current"); err != nil {
		t.Fatal(err)
	}

	if err := lifecycle.CommitProjectionManifest(t.Context(), c.active); err != nil {
		t.Fatal(err)
	}

	if err := lifecycle.Activate(t.Context(), lexicalTestScope, "current"); err != nil {
		t.Fatal(err)
	}

	assertPrivacyCount(t, store, "SELECT count(*) FROM lexical_build", 1)
	assertPrivacyCount(t, store, "SELECT count(*) FROM lexical_build WHERE generation_id='current'", 1)
}

func TestPublicationPreviousRequiresCanonicalPhysicalTuple(t *testing.T) {
	store, _ := openTestStore(t, false)
	previous := publicationManifest("previous", 1)
	c := &publicationCheckpoints{active: previous}
	a, _ := publicationLifecycle(t, store, &applyReader{}, c)
	publishRetentionGeneration(t, a, previous)
	previous.Status = memory.ProjectionGenerationSuperseded
	c.previous = []memory.ProjectionManifest{previous}
	c.active = publicationManifest("current", 2)
	publishRetentionGeneration(t, a, c.active)

	id, err := store.referencedPreviousBuildID(t.Context(), previous)
	if err != nil || id == "" {
		t.Fatalf("valid previous publication missing: %q %v", id, err)
	}

	for _, mutate := range []func(*memory.ProjectionManifest){
		func(m *memory.ProjectionManifest) { m.GenerationID = "different" },
		func(m *memory.ProjectionManifest) { m.Watermark = 2 },
		func(m *memory.ProjectionManifest) { m.Scope.Kind = memory.ScopeKindPersonal },
		func(m *memory.ProjectionManifest) { m.Scope.Key = "different" },
	} {
		other := previous
		mutate(&other)

		if id, err := store.referencedPreviousBuildID(t.Context(), other); err != nil || id != "" {
			t.Fatalf("unbound previous publication retained: %q %v", id, err)
		}
	}

	if _, err := store.writer.ExecContext(t.Context(),
		"UPDATE lexical_build SET record_count=1 WHERE physical_build_id=?", id); err != nil {
		t.Fatal(err)
	}

	if _, err := store.referencedPreviousBuildID(t.Context(), previous); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("incomplete previous publication certified: %v", err)
	}
}

func publishRetentionGeneration(t *testing.T, a *SQLiteCanonicalApplier, manifest memory.ProjectionManifest) {
	t.Helper()

	if err := a.BeginProjectionRebuild(t.Context(), manifest.Scope, manifest.GenerationID); err != nil {
		t.Fatal(err)
	}

	if err := a.CommitProjectionManifest(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}

	if err := a.Activate(t.Context(), manifest.Scope, manifest.GenerationID); err != nil {
		t.Fatal(err)
	}
}
