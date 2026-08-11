package standaloneruntime

type standaloneCleanupStack struct {
	funcs []func()
}

func (s *standaloneCleanupStack) Add(fn func()) {
	if fn != nil {
		s.funcs = append(s.funcs, fn)
	}
}

func (s *standaloneCleanupStack) Close() {
	for index := len(s.funcs) - 1; index >= 0; index-- {
		s.funcs[index]()
	}
	s.funcs = nil
}
