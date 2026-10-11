package orderedingest

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (r *SequencedIngestRuntime) IngestSequencedTurn(
	ctx context.Context,
	request memory.SequencedTurn,
) (
	memory.IngestOutcome,
	error,
) {
	if r == nil || r.turn.processor == nil {
		return memory.IngestOutcome{}, memory.PermanentError(
			memory.CodeDisabled,
			"sequenced turn ingestion is unavailable",
			nil,
		)
	}

	if err := request.Validate(); err != nil {
		return memory.IngestOutcome{}, err
	}

	var outcome memory.IngestOutcome

	err := r.lanes.Execute(ctx, request.Turn.Scope, func(runCtx context.Context) error {
		var ingestErr error

		outcome, ingestErr = r.ingestTurn(runCtx, request)

		return ingestErr
	})

	return outcome, err
}

func (r *SequencedIngestRuntime) ApplySequencedBoundary(
	ctx context.Context,
	request memory.SequencedBoundary,
) (
	memory.IngestOutcome,
	error,
) {
	if r == nil || r.boundary.processor == nil {
		return memory.IngestOutcome{}, memory.PermanentError(
			memory.CodeDisabled,
			"sequenced boundary ingestion is unavailable",
			nil,
		)
	}

	if err := request.Validate(); err != nil {
		return memory.IngestOutcome{}, err
	}

	var outcome memory.IngestOutcome

	err := r.lanes.Execute(ctx, request.Boundary.Scope, func(runCtx context.Context) error {
		var ingestErr error

		outcome, ingestErr = r.ingestBoundary(runCtx, request)

		return ingestErr
	})

	return outcome, err
}

func (r *SequencedIngestRuntime) markProjectionStatus(
	ctx context.Context,
	scope memory.Scope,
	outcome memory.IngestOutcome,
	projection ProjectionSyncer,
	repair *projectionRepairer,
) memory.IngestOutcome {
	if projection == nil {
		return outcome
	}

	if err := projection.Sync(ctx, scope); err != nil {
		outcome.Projection = memory.ProjectionStatusLagging

		if repair != nil {
			_ = repair.Mark(scope)
		}
	}

	return outcome
}
