package standaloneruntime

import "context"

func (s *standaloneService) startComponents(ctx context.Context) error {
	if err := s.maintenance.Start(ctx); err != nil {
		return err
	}

	if err := s.projection.Start(ctx); err != nil {
		return err
	}

	return s.sequenced.Start(ctx)
}
