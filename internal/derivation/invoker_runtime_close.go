package derivation

import (
	"context"
)

// Close releases resources owned by the isolated derivation runtime.
func (i *Invoker) Close(context.Context) error {
	if i == nil {
		return nil
	}

	i.mu.Lock()
	if i.closed {
		i.mu.Unlock()

		return nil
	}

	i.closed = true
	runtime := i.runtime
	i.runtime = nil
	building := i.building
	i.mu.Unlock()

	if building != nil {
		building.finish()
	}
	return closeBuiltRuntime(runtime)
}
