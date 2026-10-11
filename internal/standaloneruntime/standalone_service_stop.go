package standaloneruntime

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (s *standaloneService) Stop(ctx context.Context) error {
	if s.beginStop() {
		s.logger.Info().Msg("Boyan service stopping")
		go func() {
			s.lifecycle.Lock()
			defer s.lifecycle.Unlock()

			s.completeStop(s.stopComponents(context.Background()))
		}()
	}

	select {
	case <-s.done:
		return s.terminalResult()
	default:
	}

	select {
	case <-s.done:
		return s.terminalResult()
	case <-ctx.Done():
		return memory.RetryableError(memory.CodeTimeout, "Boyan shutdown deadline exceeded", ctx.Err())
	}
}
