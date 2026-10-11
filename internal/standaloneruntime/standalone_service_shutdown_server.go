package standaloneruntime

import (
	"context"
	"errors"
	stdhttp "net/http"
)

func (s *standaloneService) shutdownServer(ctx context.Context) error {
	err := s.server.Shutdown(ctx)
	if errors.Is(err, stdhttp.ErrServerClosed) {
		return nil
	}

	return err
}
