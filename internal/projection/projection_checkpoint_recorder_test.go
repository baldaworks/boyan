package projection

import (
	"context"
	"testing"
	"time"

	. "github.com/baldaworks/boyan/pkg/memory"
)

func TestScopeCheckpointRecorderIsIdempotentAndMonotonic(t *testing.T) {
	scope := Scope{Key: "telegram:checkpoint", Kind: ScopeKindPersonal}
	store := &memoryCheckpointStore{}
	recorder, err := NewScopeCheckpointRecorder(store)
	if err != nil {
		t.Fatalf("NewScopeCheckpointRecorder() error = %v", err)
	}
	checkpoint := ScopeCheckpoint{
		SchemaVersion: ScopeCheckpointSchemaVersion,
		Scope:         scope,
		Kind:          ScopeCheckpointTurn,
		CheckpointID:  "turn-1",
		ScopeVersion:  2,
		ChangeSeq:     2,
		OccurredAt:    checkpointTime(2),
		UpdatedAt:     checkpointTime(2),
	}
	if err := recorder.Record(context.Background(), checkpoint); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if err := recorder.Record(context.Background(), checkpoint); err != nil {
		t.Fatalf("Record(replay) error = %v", err)
	}
	checkpoint.CheckpointID = "turn-0"
	checkpoint.ScopeVersion = 1
	checkpoint.ChangeSeq = 1
	if err := recorder.Record(context.Background(), checkpoint); err == nil {
		t.Fatalf("Record(backwards) error = %v, want conflict", err)
	}
}

func checkpointTime(offset int) time.Time {
	return time.Date(2026, time.August, 6, 0, 0, offset, 0, time.UTC)
}
