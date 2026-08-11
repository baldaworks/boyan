package derivation

import (
	"context"
	"io"
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
	i.mu.Unlock()

	if runtime == nil || runtime.agent == nil {
		return nil
	}
	if closer, ok := runtime.agent.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}
