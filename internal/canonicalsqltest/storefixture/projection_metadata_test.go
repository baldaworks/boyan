//nolint:cyclop,funlen,gocognit // Real metadata lifecycles retain every transition and restart assertion.
package storefixture_test

import (
	"context"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

type metadataStore interface {
	memory.ProjectionCheckpointStore
	memory.ProjectionRetentionFloorWriter
	memory.ScopeCheckpointStore
	WalkProjectionManifests(
		ctx context.Context,
		projectionID string,
		visit func(memory.ProjectionManifest, bool) error,
	) error
}

func metadataFixture(t *testing.T, run func(*testing.T, *storefixture.Fixture, metadataStore, mutationProbe)) {
	t.Helper()
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		s, ok := f.Store.(metadataStore)
		if !ok {
			t.Fatal("SQL owner does not implement projection metadata")
		}

		run(t, f, s, newMutationProbe(t, f))
	})
}

func testManifest(t *testing.T, generation string) memory.ProjectionManifest {
	t.Helper()

	return memory.ProjectionManifest{
		Scope:        memory.Scope{Key: "test:projection:" + t.Name(), Kind: memory.ScopeKindPersonal},
		ProjectionID: "sqlite",
		GenerationID: generation,
		Status:       memory.ProjectionGenerationBuilding,
		UpdatedAt:    time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC),
	}
}

func TestPublicProjectionGenerationLifecycle(t *testing.T) {
	metadataFixture(t, func(t *testing.T, f *storefixture.Fixture, s metadataStore, p mutationProbe) {
		m := testManifest(t, "first")
		if _, found, err := s.LoadActiveProjectionManifest(t.Context(), m.Scope, m.ProjectionID); err != nil || found {
			t.Fatalf("empty=%t %v", found, err)
		}

		if err := s.MarkProjectionDirty(t.Context(), m); err != nil {
			t.Fatal(err)
		}

		if err := s.AdvanceProjectionWatermark(
			t.Context(),
			m,
			math.MaxUint64,
			m.UpdatedAt.Add(time.Second),
		); err != nil {
			t.Fatal(err)
		}

		assertMutationCode(t, s.AdvanceProjectionWatermark(t.Context(), m, 7, m.UpdatedAt), memory.CodeConflict)

		if err := s.ActivateProjectionGeneration(t.Context(), m, m.UpdatedAt); err != nil {
			t.Fatal(err)
		}

		assertMutationCode(
			t,
			s.AdvanceProjectionWatermark(t.Context(), m, math.MaxUint64, m.UpdatedAt),
			memory.CodeConflict,
		)
		assertMutationCode(t, s.ActivateProjectionGeneration(t.Context(), m, m.UpdatedAt), memory.CodeConflict)

		first, found, err := s.LoadActiveProjectionManifest(t.Context(), m.Scope, m.ProjectionID)
		if err != nil || !found || first.Status != memory.ProjectionGenerationActive ||
			first.Watermark != math.MaxUint64 {
			t.Fatalf("first=%+v %t %v", first, found, err)
		}

		if err := s.MarkProjectionDirty(t.Context(), first); err != nil {
			t.Fatal(err)
		}

		dirty, found, err := s.LoadActiveProjectionManifest(t.Context(), m.Scope, m.ProjectionID)
		if err != nil || !found || dirty.Status != memory.ProjectionGenerationDirty {
			t.Fatalf("repair=%+v %v", dirty, err)
		}

		second := m

		second.GenerationID = "second"
		if err := s.MarkProjectionDirty(t.Context(), second); err != nil {
			t.Fatal(err)
		}

		if err := s.ActivateProjectionGeneration(t.Context(), second, m.UpdatedAt.Add(2*time.Second)); err != nil {
			t.Fatal(err)
		}

		old, found, err := s.LoadProjectionManifest(t.Context(), m.Scope, m.ProjectionID, m.GenerationID)
		if err != nil || !found || old.Status != memory.ProjectionGenerationSuperseded {
			t.Fatalf("old=%+v %v", old, err)
		}

		other := second

		other.Scope.Kind = memory.ScopeKindGroup
		if err := s.MarkProjectionDirty(t.Context(), other); err != nil {
			t.Fatal(err)
		}

		if _, found, err := s.LoadActiveProjectionManifest(
			t.Context(),
			other.Scope,
			m.ProjectionID,
		); err != nil ||
			found {
			t.Fatalf("foreign pointer=%t %v", found, err)
		}

		seen := map[string]bool{}

		if err := s.WalkProjectionManifests(
			t.Context(),
			m.ProjectionID,
			func(got memory.ProjectionManifest, active bool) error {
				seen[string(got.Scope.Kind)+":"+got.GenerationID] = active

				return nil
			},
		); err != nil {
			t.Fatal(err)
		}

		if !reflect.DeepEqual(
			seen,
			map[string]bool{"personal:first": false, "personal:second": true, "group:second": false},
		) {
			t.Fatalf("walk=%v", seen)
		}

		if err := f.Store.Close(); err != nil {
			t.Fatal(err)
		}

		reopened, err := f.Open(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = reopened.Close() })

		active, found, err := reopened.(metadataStore).LoadActiveProjectionManifest(
			t.Context(),
			m.Scope,
			m.ProjectionID,
		)
		if err != nil || !found || active.GenerationID != "second" {
			t.Fatalf("reopen=%+v %v", active, err)
		}

		assertMutationState(t, reopened.(mutationStore), m.Scope, 0)

		if len(p.stamps(t)) != 4 {
			t.Fatalf("metadata stamps=%+v", p.stamps(t))
		}
	})
}

func TestPublicProjectionActivationRollsBack(t *testing.T) {
	metadataFixture(t, func(t *testing.T, _ *storefixture.Fixture, s metadataStore, p mutationProbe) {
		m := testManifest(t, "first")
		if err := s.MarkProjectionDirty(t.Context(), m); err != nil {
			t.Fatal(err)
		}

		before := p.stamps(t)
		release := p.failInserts(t, "projection_active")
		err := s.ActivateProjectionGeneration(t.Context(), m, m.UpdatedAt)

		release()

		if err == nil || !reflect.DeepEqual(before, p.stamps(t)) {
			t.Fatalf("activation escaped: %v", err)
		}

		got, found, err := s.LoadProjectionManifest(t.Context(), m.Scope, m.ProjectionID, m.GenerationID)
		if err != nil || !found || got.Status != memory.ProjectionGenerationDirty {
			t.Fatalf("rollback=%+v %v", got, err)
		}

		if _, found, err := s.LoadActiveProjectionManifest(t.Context(), m.Scope, m.ProjectionID); err != nil || found {
			t.Fatalf("pointer escaped=%t %v", found, err)
		}
	})
}

func TestPublicProjectionCheckpointAndRetentionMonotonicity(t *testing.T) {
	metadataFixture(t, func(t *testing.T, f *storefixture.Fixture, s metadataStore, p mutationProbe) {
		m := testManifest(t, "first")

		floor, err := s.LoadProjectionRetentionFloor(t.Context(), m.Scope, m.ProjectionID)
		if err != nil || floor != 0 {
			t.Fatalf("empty floor=%d %v", floor, err)
		}

		if err := s.SetProjectionRetentionFloor(t.Context(), m.Scope, m.ProjectionID, math.MaxUint64); err != nil {
			t.Fatal(err)
		}

		assertMutationCode(
			t,
			s.SetProjectionRetentionFloor(t.Context(), m.Scope, m.ProjectionID, 7),
			memory.CodeConflict,
		)

		cp := memory.ScopeCheckpoint{SchemaVersion: memory.ScopeCheckpointSchemaVersion, Scope: m.Scope,
			Kind: memory.ScopeCheckpointBoundary, CheckpointID: "boundary", ScopeVersion: 9, ChangeSeq: math.MaxUint64,
			OccurredAt: m.UpdatedAt, UpdatedAt: m.UpdatedAt.Add(time.Second)}
		if err := s.SaveScopeCheckpoint(t.Context(), cp); err != nil {
			t.Fatal(err)
		}

		before := p.stamps(t)
		replay := cp
		replay.OccurredAt = replay.OccurredAt.Add(time.Hour)

		replay.UpdatedAt = replay.UpdatedAt.Add(time.Hour)
		if err := s.SaveScopeCheckpoint(t.Context(), replay); err != nil {
			t.Fatal(err)
		}

		if !reflect.DeepEqual(before, p.stamps(t)) {
			t.Fatal("checkpoint replay wrote stamps")
		}

		for _, field := range []string{"version", "sequence", "time"} {
			stale := cp

			switch field {
			case "version":
				stale.ScopeVersion--
			case "sequence":
				stale.ChangeSeq--
			case "time":
				stale.OccurredAt = stale.OccurredAt.Add(-time.Second)
			}

			assertMutationCode(t, s.SaveScopeCheckpoint(t.Context(), stale), memory.CodeConflict)
		}

		if err := f.Store.Close(); err != nil {
			t.Fatal(err)
		}

		reopened, err := f.Open(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = reopened.Close() })

		s = reopened.(metadataStore)

		got, found, err := s.LoadScopeCheckpoint(t.Context(), m.Scope, cp.Kind)
		if err != nil || !found || got != cp {
			t.Fatalf("checkpoint=%+v %t %v", got, found, err)
		}

		floor, err = s.LoadProjectionRetentionFloor(t.Context(), m.Scope, m.ProjectionID)
		if err != nil || floor != math.MaxUint64 {
			t.Fatalf("floor=%d %v", floor, err)
		}

		if _, found, err := s.LoadScopeCheckpoint(
			t.Context(),
			m.Scope,
			memory.ScopeCheckpointTime,
		); err != nil ||
			found {
			t.Fatalf("other kind=%t %v", found, err)
		}

		assertMutationState(t, reopened.(mutationStore), m.Scope, 0)
	})
}

func TestPublicProjectionActiveCorruptionFailsClosed(t *testing.T) {
	metadataFixture(t, func(t *testing.T, _ *storefixture.Fixture, s metadataStore, p mutationProbe) {
		m := testManifest(t, "missing")
		key := canonicalsql.Key{
			Family: canonicalsql.ProjectionActive,
			Scope:  m.Scope,
			Parts:  [][]byte{[]byte(m.ProjectionID)},
		}
		p.write(t, key, map[string]string{"generation_id": m.GenerationID})
		_, _, err := s.LoadActiveProjectionManifest(t.Context(), m.Scope, m.ProjectionID)
		assertMutationCode(t, err, memory.CodeStoreFailure)
	})
}
