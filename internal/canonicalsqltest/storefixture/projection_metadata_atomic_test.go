//nolint:cyclop,funlen,gocognit // Real fault rollback and paged traversal keep transaction invariants together.
package storefixture_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func failMetadataUpdate(t *testing.T, p mutationProbe, table string) func() {
	t.Helper()

	if p.table(table) == table {
		if err := p.exec(
			"CREATE TRIGGER task11_fail BEFORE UPDATE ON " + table + " BEGIN SELECT RAISE(ABORT,'task11 injected failure'); END",
		); err != nil {
			t.Fatal(err)
		}

		return func() {
			if err := p.exec("DROP TRIGGER task11_fail"); err != nil {
				t.Fatal(err)
			}
		}
	}

	if err := p.exec(
		"CREATE FUNCTION boyan_canonical.task11_fail() RETURNS trigger LANGUAGE plpgsql AS $$ " +
			"BEGIN RAISE EXCEPTION 'task11 injected failure'; END $$",
	); err != nil {
		t.Fatal(err)
	}

	if err := p.exec(
		"CREATE TRIGGER task11_fail BEFORE UPDATE ON " + p.table(
			table,
		) + " FOR EACH ROW EXECUTE FUNCTION boyan_canonical.task11_fail()",
	); err != nil {
		t.Fatal(err)
	}

	return func() {
		if err := p.exec("DROP TRIGGER task11_fail ON " + p.table(table)); err != nil {
			t.Fatal(err)
		}

		if err := p.exec("DROP FUNCTION boyan_canonical.task11_fail()"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPublicProjectionSwitchRollbackAndStampAccounting(t *testing.T) {
	metadataFixture(t, func(t *testing.T, _ *storefixture.Fixture, s metadataStore, p mutationProbe) {
		first := testManifest(t, "first")
		if err := s.MarkProjectionDirty(t.Context(), first); err != nil {
			t.Fatal(err)
		}

		if err := s.ActivateProjectionGeneration(t.Context(), first, first.UpdatedAt); err != nil {
			t.Fatal(err)
		}

		second := first

		second.GenerationID = "second"
		if err := s.MarkProjectionDirty(t.Context(), second); err != nil {
			t.Fatal(err)
		}

		before := p.snapshot(t)

		release := failMetadataUpdate(t, p, "projection_active")
		err := s.ActivateProjectionGeneration(t.Context(), second, second.UpdatedAt)

		release()

		if err == nil || !reflect.DeepEqual(before, p.snapshot(t)) {
			t.Fatalf("failed switch escaped: %v", err)
		}

		active, found, err := s.LoadActiveProjectionManifest(t.Context(), first.Scope, first.ProjectionID)
		if err != nil || !found || active.GenerationID != "first" ||
			active.Status != memory.ProjectionGenerationActive {
			t.Fatalf("previous=%+v %v", active, err)
		}

		if err := s.ActivateProjectionGeneration(t.Context(), second, second.UpdatedAt); err != nil {
			t.Fatal(err)
		}

		active, found, err = s.LoadActiveProjectionManifest(t.Context(), second.Scope, second.ProjectionID)
		if err != nil || !found || active.GenerationID != "second" {
			t.Fatalf("active switch=%+v %v", active, err)
		}
	})
}

func TestPublicProjectionFloorAndCheckpointRollback(t *testing.T) {
	metadataFixture(t, func(t *testing.T, _ *storefixture.Fixture, s metadataStore, p mutationProbe) {
		m := testManifest(t, "first")
		for _, family := range []string{"projection_retention", "projection_checkpoint"} {
			before := p.snapshot(t)

			var err error

			release := p.failInserts(t, family)
			if family == "projection_retention" {
				err = s.SetProjectionRetentionFloor(t.Context(), m.Scope, m.ProjectionID, 7)
			} else {
				err = s.SaveScopeCheckpoint(
					t.Context(),
					memory.ScopeCheckpoint{
						SchemaVersion: memory.ScopeCheckpointSchemaVersion,
						Scope:         m.Scope,
						Kind:          memory.ScopeCheckpointTime,
						CheckpointID:  "time",
						ScopeVersion:  1,
						ChangeSeq:     7,
						OccurredAt:    m.UpdatedAt,
						UpdatedAt:     m.UpdatedAt,
					},
				)
			}

			release()

			if err == nil || !reflect.DeepEqual(before, p.snapshot(t)) {
				t.Fatalf("%s failure escaped: %v", family, err)
			}
		}
	})
}

func TestPublicProjectionWalkPagesOrderAndCallbacks(t *testing.T) {
	metadataFixture(t, func(t *testing.T, f *storefixture.Fixture, s metadataStore, p mutationProbe) {
		spoolDirectory := t.TempDir()
		t.Setenv("TMPDIR", spoolDirectory)
		m := testManifest(t, "first")

		const count = memory.MaxCanonicalReadRecords + 1
		// One raw stamped fixture transaction creates >page metadata without
		// adding unrelated canonical mutations to this maintenance test.
		if err := p.connection.Update(t.Context(), func(tx *canonicalsql.Tx) error {
			for i := range count {
				m.GenerationID = fmt.Sprintf("generation-%03d", i)
				key := canonicalsql.Key{
					Family: canonicalsql.ProjectionManifest,
					Scope:  m.Scope,
					Parts:  [][]byte{[]byte(m.ProjectionID), []byte(m.GenerationID)},
				}

				data, err := canonicalsql.Encode(key.Family, m)
				if err != nil {
					return err
				}

				if err := tx.Put(t.Context(), key, data); err != nil {
					return err
				}
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}

		if err := s.MarkProjectionDirty(t.Context(), m); err != nil {
			t.Fatal(err)
		}

		if err := s.ActivateProjectionGeneration(t.Context(), m, m.UpdatedAt); err != nil {
			t.Fatal(err)
		}

		seen := 0

		if err := s.WalkProjectionManifests(
			t.Context(),
			m.ProjectionID,
			func(got memory.ProjectionManifest, active bool) error {
				if got.GenerationID != fmt.Sprintf("generation-%03d", seen) || active != (seen == count-1) {
					t.Fatalf("visit[%d]=%+v active=%t", seen, got, active)
				}
				// A canonical read in the visitor must run after snapshot admission.
				if _, err := f.Store.(mutationStore).LoadScopeState(t.Context(), m.Scope); err != nil {
					return err
				}

				seen++

				return nil
			},
		); err != nil {
			t.Fatal(err)
		}

		if seen != count {
			t.Fatalf("visits=%d", seen)
		}

		stop := errors.New("visitor stopped")
		if err := s.WalkProjectionManifests(
			t.Context(),
			m.ProjectionID,
			func(memory.ProjectionManifest, bool) error { return stop },
		); !errors.Is(
			err,
			stop,
		) {
			t.Fatalf("visitor error=%v", err)
		}

		ctx, cancel := context.WithCancel(t.Context())
		seen = 0

		err := s.WalkProjectionManifests(ctx, m.ProjectionID, func(memory.ProjectionManifest, bool) error {
			seen++

			cancel()

			return nil
		})
		if !errors.Is(err, context.Canceled) || seen != 1 {
			t.Fatalf("cancel visits=%d err=%v", seen, err)
		}

		remaining, err := os.ReadDir(spoolDirectory)
		if err != nil || len(remaining) != 0 {
			t.Fatalf("retained spool=%v err=%v", remaining, err)
		}
	})
}
