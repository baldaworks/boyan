//nolint:testpackage,funlen,cyclop,gocyclo,gocognit,lll // Real cross-store crash windows use canonical fixtures and explicit durable assertions.
package badger

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	search "github.com/baldaworks/boyan/index/sqlite"
	"github.com/baldaworks/boyan/internal/projectionsync"
	"github.com/baldaworks/boyan/pkg/memory"
	badgerdb "github.com/dgraph-io/badger/v4"
)

type coordinatorFault struct {
	boundary string
	cancel   bool
}

func (f coordinatorFault) interrupt(_ context.Context, boundary string) error {
	if f.boundary != boundary {
		return nil
	}

	if f.cancel {
		return context.Canceled
	}

	return errors.New("publication interrupted")
}

type coordinatorCheckpoints struct {
	*CanonicalStore

	fault coordinatorFault
}

func (c *coordinatorCheckpoints) AdvanceProjectionWatermark(ctx context.Context, manifest memory.ProjectionManifest, watermark uint64, at time.Time) error {
	if err := c.fault.interrupt(ctx, "before advance"); err != nil {
		return err
	}

	if err := c.CanonicalStore.AdvanceProjectionWatermark(ctx, manifest, watermark, at); err != nil {
		return err
	}

	return c.fault.interrupt(ctx, "after advance")
}

func (c *coordinatorCheckpoints) ActivateProjectionGeneration(ctx context.Context, manifest memory.ProjectionManifest, at time.Time) error {
	if err := c.fault.interrupt(ctx, "before canonical activation"); err != nil {
		return err
	}

	if err := c.CanonicalStore.ActivateProjectionGeneration(ctx, manifest, at); err != nil {
		return err
	}

	return c.fault.interrupt(ctx, "after canonical activation")
}

type coordinatorSQLApplier struct {
	*search.SQLiteCanonicalApplier

	fault coordinatorFault
}

func (a *coordinatorSQLApplier) CommitProjectionManifest(ctx context.Context, manifest memory.ProjectionManifest) error {
	if a.fault.cancel && a.fault.boundary == "before physical commit" {
		canceled, cancel := context.WithCancel(ctx)
		cancel()

		return a.SQLiteCanonicalApplier.CommitProjectionManifest(canceled, manifest)
	}

	if err := a.SQLiteCanonicalApplier.CommitProjectionManifest(ctx, manifest); err != nil {
		return err
	}

	return a.fault.interrupt(ctx, "after physical commit")
}

func (a *coordinatorSQLApplier) Activate(ctx context.Context, scope memory.Scope, generation string) error {
	if a.fault.cancel && a.fault.boundary == "before SQL activation" {
		canceled, cancel := context.WithCancel(ctx)
		cancel()

		return a.SQLiteCanonicalApplier.Activate(canceled, scope, generation)
	}

	if err := a.SQLiteCanonicalApplier.Activate(ctx, scope, generation); err != nil {
		return err
	}

	return a.fault.interrupt(ctx, "after SQL activation")
}

func coordinatorCanonicalFixture(t *testing.T) (*CanonicalStore, *CanonicalReader, memory.Scope, string) {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "canonical")

	store, err := OpenCanonicalStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	scope := memory.Scope{Key: "canonical:coordinator", Kind: memory.ScopeKindPersonal}
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	turn := canonicalReaderTestTurn(t, scope, now)

	evidence, err := memory.NewEvidenceRef(turn.SourceID, turn.Messages[0].MessageID, memory.MessageRoleUser,
		turn.Messages[0].Text, 0, uint32(len(turn.Messages[0].Text)), memory.AssertionModeUser)
	if err != nil {
		t.Fatal(err)
	}

	mutation := canonicalReaderTestMutation(scope, now, evidence, turn)
	source := canonicalReaderTestSourcePayload(t, turn)
	mutation.Sources[0].Payload = payloadRef("source", source)
	mutation.Messages[0].Payload = payloadRef("user", []byte(turn.Messages[0].Text))
	mutation.Messages[1].Payload = payloadRef("assistant", []byte(turn.Messages[1].Text))
	mutation.Revisions[0].Payload = payloadRef("revision", []byte("canonical result"))

	mutation.Payloads = []memory.CanonicalPayload{
		{Ref: mutation.Sources[0].Payload, Data: source},
		{Ref: mutation.Messages[0].Payload, Data: []byte(turn.Messages[0].Text)},
		{Ref: mutation.Messages[1].Payload, Data: []byte(turn.Messages[1].Text)},
		{Ref: mutation.Revisions[0].Payload, Data: []byte("canonical result")},
	}
	if _, err := store.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
		t.Fatal(err)
	}

	reader, err := NewCanonicalReader(store)
	if err != nil {
		t.Fatal(err)
	}

	return store, reader, scope, dir
}

func coordinatorProjection(t *testing.T, dir string, reader memory.RecallCanonicalReader, checkpoints memory.ProjectionCheckpointStore, canonical *CanonicalStore) (*search.Store, *search.SQLiteCanonicalApplier) {
	t.Helper()

	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	store, err := search.Open(t.Context(), dir, search.Options{})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	applier, err := search.NewSQLiteCanonicalApplier(store, reader, canonical)
	if err != nil {
		t.Fatal(err)
	}

	applier.ConfigureProjectionRetention(checkpoints, "sqlite")

	return store, applier
}

func coordinatorOperationBytes(t *testing.T, store *CanonicalStore, scope memory.Scope) []byte {
	t.Helper()

	key, err := badgerOperationKey(scope, "operation-reader")
	if err != nil {
		t.Fatal(err)
	}

	var result []byte

	if err := store.db.View(func(tx *badgerdb.Txn) error {
		item, err := tx.Get(key)
		if err != nil {
			return err
		}

		result, err = item.ValueCopy(nil)

		return err
	}); err != nil {
		t.Fatal(err)
	}

	return result
}

func TestSQLiteCoordinatorPublicationInterruptionRecovery(t *testing.T) {
	for _, boundary := range []string{"before physical commit", "after physical commit", "before advance", "after advance",
		"before canonical activation", "after canonical activation", "before SQL activation", "after SQL activation"} {
		for _, cancel := range []bool{false, true} {
			name := boundary
			if cancel {
				name += " cancellation"
			}

			t.Run(name, func(t *testing.T) {
				canonical, reader, scope, canonicalDir := coordinatorCanonicalFixture(t)
				receipt := coordinatorOperationBytes(t, canonical, scope)
				fault := coordinatorFault{boundary: boundary, cancel: cancel}
				checkpoints := &coordinatorCheckpoints{CanonicalStore: canonical, fault: fault}
				dir := t.TempDir()
				projection, applier := coordinatorProjection(t, dir, reader, checkpoints, canonical)

				db, err := sql.Open("sqlite", filepath.Join(dir, "search", "search.sqlite"))
				if err != nil {
					t.Fatal(err)
				}

				if !cancel && (boundary == "before physical commit" || boundary == "before SQL activation") {
					trigger := `CREATE TRIGGER fail_publication BEFORE UPDATE OF state ON lexical_build WHEN NEW.state='committed' BEGIN SELECT RAISE(ABORT,'fault'); END`
					if boundary == "before SQL activation" {
						trigger = `CREATE TRIGGER fail_publication BEFORE INSERT ON lexical_active BEGIN SELECT RAISE(ABORT,'fault'); END`
					}

					if _, err := db.ExecContext(t.Context(), trigger); err != nil {
						t.Fatal(err)
					}
				}

				coordinator, err := projectionsync.NewProjectionCoordinator(canonical, checkpoints,
					&coordinatorSQLApplier{SQLiteCanonicalApplier: applier, fault: fault}, 1)
				if err != nil {
					t.Fatal(err)
				}

				if _, err := coordinator.Sync(t.Context(), scope, "sqlite", "g"); err == nil || (cancel && !errors.Is(err, context.Canceled)) {
					t.Fatalf("interrupted sync error = %v", err)
				}

				manifest, found, err := canonical.LoadProjectionManifest(t.Context(), scope, "sqlite", "g")
				if err != nil || !found {
					t.Fatalf("durable checkpoint missing: %+v %v %v", manifest, found, err)
				}

				wantActive := boundary == "after canonical activation" || boundary == "before SQL activation" || boundary == "after SQL activation"
				if (manifest.Status == memory.ProjectionGenerationActive) != wantActive {
					t.Fatalf("checkpoint ordering at %s: %+v", boundary, manifest)
				}

				wantWatermark := uint64(1)
				if boundary == "before physical commit" || boundary == "after physical commit" || boundary == "before advance" {
					wantWatermark = 0
				}

				if manifest.Watermark != wantWatermark {
					t.Fatalf("checkpoint advanced across failed boundary: %+v", manifest)
				}

				_, certified, err := projection.LoadActivePhysicalProjectionManifest(t.Context(), scope, "sqlite")
				if err != nil || certified != (boundary == "after SQL activation") {
					t.Fatalf("premature physical certification: %v %v", certified, err)
				}

				if _, err := db.ExecContext(t.Context(), `DROP TRIGGER IF EXISTS fail_publication`); err != nil {
					t.Fatal(err)
				}

				if err := db.Close(); err != nil {
					t.Fatal(err)
				}

				if err := projection.Close(); err != nil {
					t.Fatal(err)
				}

				if err := canonical.Close(); err != nil {
					t.Fatal(err)
				}

				canonical, err = OpenCanonicalStore(canonicalDir)
				if err != nil {
					t.Fatal(err)
				}

				t.Cleanup(func() { _ = canonical.Close() })

				reader, err = NewCanonicalReader(canonical)
				if err != nil {
					t.Fatal(err)
				}

				projection, applier = coordinatorProjection(t, dir, reader, canonical, canonical)

				resumed, err := applier.ResumeProjection(t.Context(), scope, "g")
				if err != nil || resumed != wantActive {
					t.Fatalf("reopen resume = %v %v, canonical active = %v", resumed, err, wantActive)
				}

				if !resumed {
					coordinator, err := projectionsync.NewProjectionCoordinator(canonical, canonical, applier, 1)
					if err != nil {
						t.Fatal(err)
					}

					if _, err := coordinator.Rebuild(t.Context(), scope, "sqlite", "repaired"); err != nil {
						t.Fatal(err)
					}
				}

				at := time.Date(2026, time.October, 16, 0, 0, 0, 0, time.UTC)

				hits, err := projection.SearchRecallConsistent(t.Context(), memory.RecallRequest{Scope: scope, Query: "canonical", Limit: 1, MinScopeChangeSeq: 1, AsOf: &at})
				if err != nil || len(hits) != 1 || hits[0].RevisionID != "revision-reader" {
					t.Fatalf("repaired certified retrieval: %+v %v", hits, err)
				}

				if !bytes.Equal(receipt, coordinatorOperationBytes(t, canonical, scope)) {
					t.Fatal("canonical operation receipt bytes changed")
				}
			})
		}
	}
}

func TestSQLiteCoordinatorEmptySyncCannotCertifyLostPhysicalState(t *testing.T) {
	canonical, reader, scope, _ := coordinatorCanonicalFixture(t)
	projection, applier := coordinatorProjection(t, t.TempDir(), reader, canonical, canonical)

	coordinator, err := projectionsync.NewProjectionCoordinator(canonical, canonical, applier, 1)
	if err != nil {
		t.Fatal(err)
	}

	manifest, err := coordinator.Sync(t.Context(), scope, "sqlite", "g")
	if err != nil {
		t.Fatal(err)
	}

	if err := canonical.MarkProjectionDirty(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}

	if _, err := coordinator.Sync(t.Context(), scope, "sqlite", "g"); err != nil {
		t.Fatal("empty sync from complete active source", err)
	}
	// A fresh SQL directory models lost disposable state; retain the real
	// canonical checkpoint and its populated canonical source.
	if err := projection.Close(); err != nil {
		t.Fatal(err)
	}

	if err := canonical.MarkProjectionDirty(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}

	projection, applier = coordinatorProjection(t, t.TempDir(), reader, canonical, canonical)

	coordinator, err = projectionsync.NewProjectionCoordinator(canonical, canonical, applier, 1)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := coordinator.Sync(t.Context(), scope, "sqlite", "g"); err == nil {
		t.Fatal("lost SQL falsely certified an empty generation at retained watermark")
	}

	if _, found, err := projection.LoadActivePhysicalProjectionManifest(t.Context(), scope, "sqlite"); err != nil || found {
		t.Fatalf("lost SQL certified: %v %v", found, err)
	}

	if err := applier.AbortProjectionSync(t.Context(), "g"); err != nil {
		t.Fatal(err)
	}

	if _, err := coordinator.Rebuild(t.Context(), scope, "sqlite", "rebuilt"); err != nil {
		t.Fatal(err)
	}

	hits, err := projection.SearchRecallConsistent(t.Context(), memory.RecallRequest{Scope: scope, Query: "canonical", Limit: 1, MinScopeChangeSeq: 1})
	if err != nil || len(hits) != 1 {
		t.Fatalf("lost SQL rebuild: %+v %v", hits, err)
	}
}

func TestSQLiteCoordinatorDeltaCannotReplaceLostBase(t *testing.T) {
	canonical, reader, scope, _ := coordinatorCanonicalFixture(t)
	_, applier := coordinatorProjection(t, t.TempDir(), reader, canonical, canonical)

	coordinator, err := projectionsync.NewProjectionCoordinator(canonical, canonical, applier, 1)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := coordinator.Sync(t.Context(), scope, "sqlite", "g"); err != nil {
		t.Fatal(err)
	}
	// A second real canonical mutation updates only another item. Replaying its
	// delta into lost SQL must not claim to have copied the first item.
	coordinatorNextRevision(t, canonical, scope, false)

	_, applier = coordinatorProjection(t, t.TempDir(), reader, canonical, canonical)

	coordinator, err = projectionsync.NewProjectionCoordinator(canonical, canonical, applier, 1)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := coordinator.Sync(t.Context(), scope, "sqlite", "g"); err == nil {
		t.Fatal("delta falsely certified missing retained base")
	}

	manifest, found, err := canonical.LoadProjectionManifest(t.Context(), scope, "sqlite", "g")
	if err != nil || !found || manifest.Watermark != 1 || manifest.Status != memory.ProjectionGenerationDirty {
		t.Fatalf("missing base advanced: %+v %v %v", manifest, found, err)
	}
}

func coordinatorNextRevision(t *testing.T, store *CanonicalStore, scope memory.Scope, replace bool) {
	t.Helper()

	revisions, err := store.LoadCanonicalRevisions(t.Context(), memory.CanonicalRevisionReadRequest{Scope: scope, RevisionIDs: []string{"revision-reader"}})
	if err != nil || len(revisions) != 1 {
		t.Fatalf("load source revision: %+v %v", revisions, err)
	}

	revision := revisions[0]
	revision.RevisionID = "second-revision"

	revision.ItemID = "second-item"
	if replace {
		revision.ItemID = "item-reader"
		revision.Revision = 2
	}

	text := []byte("replacement")
	revision.Payload = payloadRef("second-payload", text)

	mutation := canonicalRevisionMutation(scope, 1, "second-operation", revision)
	if replace {
		mutation.Items = nil
	}

	mutation.Payloads = []memory.CanonicalPayload{{Ref: revision.Payload, Data: text}}
	if _, err := store.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteCoordinatorRetentionRebuildReplacesOldRows(t *testing.T) {
	canonical, reader, scope, _ := coordinatorCanonicalFixture(t)
	projection, applier := coordinatorProjection(t, t.TempDir(), reader, canonical, canonical)

	coordinator, err := projectionsync.NewProjectionCoordinator(canonical, canonical, applier, 1)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := coordinator.Sync(t.Context(), scope, "sqlite", "g"); err != nil {
		t.Fatal(err)
	}

	coordinatorNextRevision(t, canonical, scope, true)

	if err := canonical.SetProjectionRetentionFloor(t.Context(), scope, "sqlite", 2); err != nil {
		t.Fatal(err)
	}

	manifest, err := coordinator.Sync(t.Context(), scope, "sqlite", "g")
	if err != nil || manifest.Watermark != 2 || manifest.Status != memory.ProjectionGenerationActive {
		t.Fatalf("retention rebuild: %+v %v", manifest, err)
	}

	at := time.Date(2026, time.October, 16, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		query string
		want  int
	}{{"canonical", 0}, {"replacement", 1}} {
		hits, err := projection.SearchRecallConsistent(t.Context(), memory.RecallRequest{Scope: scope, Query: tc.query, Limit: 1, MinScopeChangeSeq: 2, AsOf: &at})
		if err != nil || len(hits) != tc.want {
			t.Fatalf("retention retrieval %s: %+v %v", tc.query, hits, err)
		}
	}
}

func TestSQLiteCoordinatorPublishesProvenEmptyScope(t *testing.T) {
	canonical, reader, _, _ := coordinatorCanonicalFixture(t)
	scope := memory.Scope{Key: "canonical:empty", Kind: memory.ScopeKindGroup}
	projection, applier := coordinatorProjection(t, t.TempDir(), reader, canonical, canonical)

	coordinator, err := projectionsync.NewProjectionCoordinator(canonical, canonical, applier, 1)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := coordinator.Sync(t.Context(), scope, "sqlite", "fresh"); err != nil {
		t.Fatal(err)
	}
	// Set a valid canonical header directly to exercise the full uint64 domain
	// without manufacturing 2^64 canonical mutations.
	key, err := badgerScopeKey(scope)
	if err != nil {
		t.Fatal(err)
	}

	if err := canonical.db.Update(func(tx *badgerdb.Txn) error {
		return putCanonicalRecord(tx, key, badgerRecordScope, memory.ScopeState{
			SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: scope, Version: 1, ChangeSeq: 18446744073709551615,
		})
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := coordinator.Rebuild(t.Context(), scope, "sqlite", "rebuilt"); err != nil {
		t.Fatal(err)
	}

	manifest, found, err := projection.LoadActivePhysicalProjectionManifest(t.Context(), scope, "sqlite")
	if err != nil || !found || manifest.GenerationID != "rebuilt" || manifest.Watermark != 18446744073709551615 {
		t.Fatalf("empty scope physical certificate: %+v %v %v", manifest, found, err)
	}
}

type coordinatorConcurrentStore struct {
	*CanonicalStore

	advance func()
	loads   int
}

func (s *coordinatorConcurrentStore) LoadScopeState(ctx context.Context, scope memory.Scope) (memory.ScopeState, error) {
	s.loads++
	if s.loads == 2 {
		s.advance()
	}

	return s.CanonicalStore.LoadScopeState(ctx, scope)
}

type coordinatorAnchorApplier struct {
	*search.SQLiteCanonicalApplier

	watermarks []uint64
}

func (a *coordinatorAnchorApplier) CommitProjectionManifest(ctx context.Context, manifest memory.ProjectionManifest) error {
	a.watermarks = append(a.watermarks, manifest.Watermark)

	return a.SQLiteCanonicalApplier.CommitProjectionManifest(ctx, manifest)
}

func TestSQLiteCoordinatorRebuildSealsInitialAnchorBeforeNewerPages(t *testing.T) {
	canonical, reader, scope, _ := coordinatorCanonicalFixture(t)
	projection, applier := coordinatorProjection(t, t.TempDir(), reader, canonical, canonical)
	store := &coordinatorConcurrentStore{CanonicalStore: canonical,
		advance: func() { coordinatorNextRevision(t, canonical, scope, false) }}
	observed := &coordinatorAnchorApplier{SQLiteCanonicalApplier: applier}

	coordinator, err := projectionsync.NewProjectionCoordinator(store, canonical, observed, 1)
	if err != nil {
		t.Fatal(err)
	}

	manifest, err := coordinator.Rebuild(t.Context(), scope, "sqlite", "g")
	if err != nil || manifest.Watermark != 2 {
		t.Fatalf("concurrent rebuild: %+v %v", manifest, err)
	}

	if !slices.Equal(observed.watermarks, []uint64{1, 2}) {
		t.Fatalf("physical targets = %v, want captured anchor1 then replay2", observed.watermarks)
	}

	at := time.Date(2026, time.October, 16, 0, 0, 0, 0, time.UTC)

	hits, err := projection.SearchRecallConsistent(t.Context(), memory.RecallRequest{Scope: scope, Query: "canonical replacement", Limit: 2, MinScopeChangeSeq: 2, AsOf: &at})
	if err != nil || len(hits) != 2 || hits[0].ScopeChangeSeq != 2 || hits[1].ScopeChangeSeq != 2 {
		t.Fatalf("newer canonical pages: %+v %v", hits, err)
	}
}
