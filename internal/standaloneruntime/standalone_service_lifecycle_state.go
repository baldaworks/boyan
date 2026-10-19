package standaloneruntime

import (
	"context"
	"errors"
	stdhttp "net/http"
	"time"
)

func (s *standaloneService) beginStart() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.starting || s.started || s.closing {
		return false
	}

	s.starting = true

	return true
}

func (s *standaloneService) finishStart() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.starting = false
	s.started = true
}

func (s *standaloneService) failStart() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.starting = false
}

func (s *standaloneService) beginStop() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closing {
		return false
	}

	s.closing = true
	s.cancelOperations()

	return true
}

func (s *standaloneService) setServing(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.serving = err
}

func (s *standaloneService) startedState() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.started || s.closing
}

func (s *standaloneService) ready() bool {
	s.mu.Lock()
	ready := s.started && !s.closing && s.serving == nil
	s.mu.Unlock()

	if !ready {
		return false
	}

	ctx, cancel := context.WithTimeout(s.shutdownContext, time.Second)
	defer cancel()

	return s.store.CheckReadiness(ctx) == nil
}

func (s *standaloneService) completeStop(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.serving != nil && !errors.Is(s.serving, stdhttp.ErrServerClosed) {
		err = errors.Join(err, s.serving)
	}

	s.terminal = err
	close(s.done)
}

func (s *standaloneService) terminalResult() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.terminal
}
