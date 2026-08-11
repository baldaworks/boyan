package standaloneruntime

import (
	"context"
)

func (s *standaloneService) Start(ctx context.Context) error {
	if !s.beginStart() {
		return nil
	}
	if err := s.startComponents(ctx); err != nil {
		s.failStart()
		return err
	}
	listener, err := s.openStandaloneListener()
	if err != nil {
		s.rollbackStartedComponents()
		s.failStart()
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
	return s.server.Addr
}
