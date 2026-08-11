package standaloneruntime

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
	if s.closing || (!s.starting && !s.started) {
		return false
	}
	s.closing = true
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
	defer s.mu.Unlock()
	return s.started && !s.closing && s.serving == nil
}
