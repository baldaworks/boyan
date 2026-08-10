package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	stdhttp "net/http"
)

func (s *standaloneService) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil
	}
	s.started = true
	s.mu.Unlock()

	if err := s.maintenance.Start(ctx); err != nil {
		return err
	}
	if err := s.projection.Start(ctx); err != nil {
		_ = s.maintenance.Close(context.Background())
		return err
	}
	if err := s.sequenced.Start(ctx); err != nil {
		_ = s.projection.Close(context.Background())
		_ = s.maintenance.Close(context.Background())
		return err
	}
	listener, err := net.Listen("tcp", s.cfg.HTTP.Listen)
	if err != nil {
		_ = s.sequenced.Close(context.Background())
		_ = s.projection.Close(context.Background())
		_ = s.maintenance.Close(context.Background())
		return fmt.Errorf("listen on %s: %w", s.cfg.HTTP.Listen, err)
	}
	s.server.Addr = listener.Addr().String()
	s.logger.Info().
		Str("listen", listener.Addr().String()).
		Str("state_dir", s.state.Root).
		Msg("Boyan service starting")
	go func() {
		err := s.server.Serve(listener)
		s.mu.Lock()
		s.serving = err
		s.mu.Unlock()
		s.done <- err
	}()
	return nil
}

func (s *standaloneService) Stop(ctx context.Context) error {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return nil
	}
	s.closing = true
	s.mu.Unlock()

	s.logger.Info().Msg("Boyan service stopping")
	var firstErr error
	if err := s.server.Shutdown(ctx); err != nil && !errors.Is(err, stdhttp.ErrServerClosed) && firstErr == nil {
		firstErr = err
	}
	if err := s.sequenced.Close(ctx); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := s.projection.Close(ctx); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := s.invoker.Close(ctx); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := s.applier.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := s.maintenance.Close(ctx); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func (s *standaloneService) address() string {
	if s == nil || s.server == nil {
		return ""
	}
	return s.server.Addr
}
