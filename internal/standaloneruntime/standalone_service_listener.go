package standaloneruntime

import (
	"context"
	"errors"
	"fmt"
	"net"
	stdhttp "net/http"
)

func (s *standaloneService) openStandaloneListener() (net.Listener, error) {
	listener, err := net.Listen("tcp", s.cfg.HTTP.Listen)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", s.cfg.HTTP.Listen, err)
	}

	return listener, nil
}

func (s *standaloneService) serveStandaloneListener(listener net.Listener) {
	s.mu.Lock()
	s.server.Addr = listener.Addr().String()
	s.mu.Unlock()

	s.logger.Info().
		Str("listen", listener.Addr().String()).
		Str("state_dir", s.state.Root).
		Msg("Boyan service starting")
	go func() {
		err := s.server.Serve(listener)
		s.setServing(err)

		if !errors.Is(err, stdhttp.ErrServerClosed) {
			_ = s.Stop(context.Background())
		}
	}()
}
