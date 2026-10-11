package bleve

// Close closes unactivated build generations and the active projection.
func (a *BleveCanonicalApplier) Close() error {
	a.mu.Lock()

	generations := make([]*BleveGeneration, 0, len(a.generations))
	for id, generation := range a.generations {
		generations = append(generations, generation)
		delete(a.generations, id)
	}
	a.mu.Unlock()

	var first error
	for _, generation := range generations {
		if err := generation.Close(); err != nil && first == nil {
			first = err
		}
	}

	if err := a.projection.Close(); err != nil && first == nil {
		first = err
	}

	return first
}
