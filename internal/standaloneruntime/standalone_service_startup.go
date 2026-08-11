package standaloneruntime

import "context"

func (s *standaloneService) startComponents(ctx context.Context) error {
	cleanups := standaloneCleanupStack{}
	if err := s.maintenance.Start(ctx); err != nil {
		return err
	}
	cleanups.Add(func() { _ = s.maintenance.Close(context.Background()) })
	if err := s.projection.Start(ctx); err != nil {
		cleanups.Close()
		return err
	}
	cleanups.Add(func() { _ = s.projection.Close(context.Background()) })
	if err := s.sequenced.Start(ctx); err != nil {
		cleanups.Close()
		return err
	}
	return nil
}

func (s *standaloneService) rollbackStartedComponents() {
	cleanups := standaloneCleanupStack{}
	cleanups.Add(func() { _ = s.maintenance.Close(context.Background()) })
	cleanups.Add(func() { _ = s.projection.Close(context.Background()) })
	cleanups.Add(func() { _ = s.sequenced.Close(context.Background()) })
	cleanups.Close()
}
