package bleve

import (
	"context"
)

func (a *BleveCanonicalApplier) generation(ctx context.Context, id string) (*BleveGeneration, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if generation := a.generations[id]; generation != nil {
		return generation, nil
	}
	generation, err := a.projection.NewGeneration(id)
	if err != nil {
		return nil, err
	}
	a.generations[id] = generation
	return generation, nil
}
