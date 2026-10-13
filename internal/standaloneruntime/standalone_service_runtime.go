package standaloneruntime

import (
	"context"
	"fmt"
)

func (s *standaloneService) Start(ctx context.Context) error {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()

	s.mu.Lock()
	closed := s.closing
	s.mu.Unlock()

	if closed {
		return fmt.Errorf("Boyan runtime is stopped")
	}

	if !s.beginStart() {
		return nil
	}

	if err := s.startComponentsUntilShutdown(ctx); err != nil {
		s.failStart()

		if s.beginStop() {
			s.completeStop(s.stopComponents(context.Background()))
		}

		return err
	}

	listener, err := s.openStandaloneListener()
	if err != nil {
		s.failStart()

		if s.beginStop() {
			s.completeStop(s.stopComponents(context.Background()))
		}

		return err
	}

	s.serveStandaloneListener(listener)
	s.finishStart()

	return nil
}

func (s *standaloneService) address() string {
	if s == nil || s.server == nil {
		return ""
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.server.Addr
}

// The startup context remains alive for background components until caller
// cancellation or shutdown. Canceling it when Start returns would stop repairs.
func (s *standaloneService) startComponentsUntilShutdown(ctx context.Context) error {
	startup, cancel := context.WithCancel(ctx)

	stop := context.AfterFunc(s.shutdownContext, cancel)
	if s.shutdownContext.Err() != nil {
		cancel()
	}

	err := s.startComponents(startup)
	if err == nil {
		err = startup.Err()
	}

	if err != nil {
		stop()
		cancel()
	}

	return err
}
