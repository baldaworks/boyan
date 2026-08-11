package memoryops

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (s *ForgetService) syncProjection(ctx context.Context, scope memory.Scope, outcome memory.ForgetOutcome, err error) (memory.ForgetOutcome, error) {
	if err != nil || s.projection == nil {
		return outcome, err
	}
	if projectErr := s.projection.Sync(ctx, scope); projectErr != nil {
		return outcome, projectErr
	}
	return outcome, nil
}
