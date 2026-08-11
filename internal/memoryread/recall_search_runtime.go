package memoryread

import (
	"context"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (s *RecallService) recallNow() time.Time {
	now := time.Now().UTC()
	if s.now != nil {
		now = s.now().UTC()
	}
	return now
}

func (s *RecallService) currentRecallScopeChangeSeq(ctx context.Context, scope memory.Scope, minScopeChangeSeq uint64) (uint64, error) {
	currentSeq, err := s.canonical.CurrentScopeChangeSeq(ctx, scope)
	if err != nil {
		return 0, err
	}
	if currentSeq < minScopeChangeSeq {
		return 0, memory.RetryableError(memory.CodeConflict, "recall consistency watermark has not caught up", nil)
	}
	return currentSeq, nil
}
