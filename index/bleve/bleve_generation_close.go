package bleve

// Close closes a build generation. It does not activate it.
func (g *BleveGeneration) Close() error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil
	}
	g.closed = true
	if g.index == nil {
		return nil
	}
	err := g.index.Close()
	g.index = nil
	return err
}
