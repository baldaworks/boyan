package projection //nolint:testpackage // Exercises shared private test fixtures within the package.

import (
	"context"

	. "github.com/baldaworks/boyan/pkg/memory"
)

var errCheckpointBackwards = PermanentError(CodeConflict, "scope checkpoint moved backwards", nil)

type memoryCheckpointStore struct {
	checkpoint ScopeCheckpoint
	found      bool
}

func (s *memoryCheckpointStore) LoadScopeCheckpoint(
	context.Context,
	Scope,
	ScopeCheckpointKind,
) (
	ScopeCheckpoint,
	bool,
	error,
) {
	return s.checkpoint, s.found, nil
}

func (s *memoryCheckpointStore) SaveScopeCheckpoint(_ context.Context, checkpoint ScopeCheckpoint) error {
	if s.found &&
		(checkpoint.ScopeVersion < s.checkpoint.ScopeVersion ||
			checkpoint.ChangeSeq < s.checkpoint.ChangeSeq) {
		return errCheckpointBackwards
	}

	s.checkpoint = checkpoint
	s.found = true

	return nil
}
