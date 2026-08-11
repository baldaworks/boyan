package standaloneruntime

import (
	"context"
)

func (s *standaloneService) Stop(ctx context.Context) error {
	if !s.beginStop() {
		return nil
	}
	s.logger.Info().Msg("Boyan service stopping")
	return s.stopComponents(ctx)
}
