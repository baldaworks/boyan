package standaloneruntime

import (
	"context"
	"fmt"

	"github.com/baldaworks/boyan/internal/forgetops"
)

func (s *standaloneService) startComponents(ctx context.Context) error {
	if err := s.store.ResumeRestoreBeforeEraseRecovery(ctx); err != nil {
		return err
	}

	if err := s.store.RestorePreflight(ctx); err != nil {
		return err
	}

	if err := forgetops.RecoverPendingScopeErases(ctx, s.store); err != nil {
		return err
	}

	if err := s.store.PrepareSourcePrivacy(ctx); err != nil {
		return err
	}

	if err := s.index.ReconcileAllCanonicalDenials(ctx); err != nil {
		return fmt.Errorf("retire Boyan SQLite canonical denials: %w; stop all writers, back up search and canonical state, "+
			"then rebuild only disposable search state", err)
	}

	if err := s.projection.Start(ctx); err != nil {
		return fmt.Errorf("repair Boyan SQLite projection: %w; stop all writers, back up search and canonical state, "+
			"then rebuild only disposable search state", err)
	}

	if err := s.sequenced.Start(ctx); err != nil {
		return err
	}

	if s.semantic != nil {
		s.semantic.start(ctx)
	}

	return nil
}
