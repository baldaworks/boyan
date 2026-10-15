package standaloneruntime

import (
	"context"

	"github.com/baldaworks/boyan/internal/forgetops"
)

func (s *standaloneService) startComponents(ctx context.Context) error {
	if err := s.maintenance.Start(ctx); err != nil {
		return err
	}

	if err := forgetops.RecoverPendingScopeErases(ctx, s.store); err != nil {
		return err
	}

	if err := s.store.PrepareSourcePrivacy(ctx); err != nil {
		return err
	}

	if err := s.projection.Start(ctx); err != nil {
		return err
	}

	if err := s.sequenced.Start(ctx); err != nil {
		return err
	}

	if s.semantic != nil {
		s.semantic.start(ctx)
	}

	return nil
}
