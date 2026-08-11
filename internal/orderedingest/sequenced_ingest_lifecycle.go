package orderedingest

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (r *SequencedIngestRuntime) Start(ctx context.Context) error {
	if r == nil {
		return memory.PermanentError(memory.CodeDisabled, "sequenced ingest runtime is unavailable", nil)
	}
	if err := r.lanes.Start(ctx); err != nil {
		return err
	}
	if r.turn.repair != nil {
		if err := r.turn.repair.Start(ctx); err != nil {
			_ = r.lanes.Close(context.Background())
			return err
		}
	}
	if r.boundary.repair != nil && r.boundary.repair != r.turn.repair {
		if err := r.boundary.repair.Start(ctx); err != nil {
			if r.turn.repair != nil {
				_ = r.turn.repair.Close(context.Background())
			}
			_ = r.lanes.Close(context.Background())
			return err
		}
	}
	return nil
}

func (r *SequencedIngestRuntime) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	var firstErr error
	if r.boundary.repair != nil && r.boundary.repair != r.turn.repair {
		if err := r.boundary.repair.Close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if r.turn.repair != nil {
		if err := r.turn.repair.Close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := r.lanes.Close(ctx); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}
