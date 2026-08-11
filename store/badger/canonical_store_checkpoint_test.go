package badger

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

func TestCanonicalStorePersistsProjectionRetentionAndScopeCheckpoints(t *testing.T) {
	store, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "memory.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	scope := boyan.Scope{Key: "canonical:projection-checkpoint", Kind: boyan.ScopeKindPersonal}
	if err := store.SetProjectionRetentionFloor(ctx, scope, "bleve", 7); err != nil {
		t.Fatalf("SetProjectionRetentionFloor() error = %v", err)
	}
	floor, err := store.LoadProjectionRetentionFloor(ctx, scope, "bleve")
	if err != nil || floor != 7 {
		t.Fatalf("LoadProjectionRetentionFloor() = %d, error %v", floor, err)
	}
	if err := store.SetProjectionRetentionFloor(ctx, scope, "bleve", 6); err == nil {
		t.Fatal("SetProjectionRetentionFloor() accepted a backwards floor")
	}
	checkpoint := boyan.ScopeCheckpoint{
		SchemaVersion: boyan.ScopeCheckpointSchemaVersion,
		Scope:         scope,
		Kind:          boyan.ScopeCheckpointBoundary,
		CheckpointID:  "boundary-1",
		ScopeVersion:  3,
		ChangeSeq:     7,
		OccurredAt:    time.Date(2026, time.August, 6, 1, 0, 0, 0, time.UTC),
		UpdatedAt:     time.Date(2026, time.August, 6, 1, 0, 1, 0, time.UTC),
	}
	if err := store.SaveScopeCheckpoint(ctx, checkpoint); err != nil {
		t.Fatalf("SaveScopeCheckpoint() error = %v", err)
	}
	if err := store.SaveScopeCheckpoint(ctx, checkpoint); err != nil {
		t.Fatalf("SaveScopeCheckpoint(replay) error = %v", err)
	}
	got, found, err := store.LoadScopeCheckpoint(ctx, scope, boyan.ScopeCheckpointBoundary)
	if err != nil || !found || got != checkpoint {
		t.Fatalf("LoadScopeCheckpoint() = %+v, found %v, error %v; want %+v, true", got, found, err, checkpoint)
	}
	checkpoint.ChangeSeq = 6
	if err := store.SaveScopeCheckpoint(ctx, checkpoint); err == nil {
		t.Fatal("SaveScopeCheckpoint() accepted a backwards cursor")
	}
}

func TestCanonicalStoreReopensScopeCheckpoint(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "memory.badger")
	store, err := OpenCanonicalStore(directory)
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	scope := boyan.Scope{Key: "canonical:checkpoint-reopen", Kind: boyan.ScopeKindPersonal}
	checkpoint := boyan.ScopeCheckpoint{
		SchemaVersion: boyan.ScopeCheckpointSchemaVersion,
		Scope:         scope,
		Kind:          boyan.ScopeCheckpointTime,
		CheckpointID:  "timer-1",
		ScopeVersion:  1,
		ChangeSeq:     1,
		OccurredAt:    time.Date(2026, time.August, 6, 2, 0, 0, 0, time.UTC),
		UpdatedAt:     time.Date(2026, time.August, 6, 2, 0, 1, 0, time.UTC),
	}
	if err := store.SaveScopeCheckpoint(context.Background(), checkpoint); err != nil {
		t.Fatalf("SaveScopeCheckpoint() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	store, err = OpenCanonicalStore(directory)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	got, found, err := store.LoadScopeCheckpoint(context.Background(), scope, boyan.ScopeCheckpointTime)
	if err != nil || !found || got != checkpoint {
		t.Fatalf("LoadScopeCheckpoint(after reopen) = %+v, found %v, error %v; want %+v, true", got, found, err, checkpoint)
	}
}
