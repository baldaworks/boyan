package structuredderive

import memory "github.com/baldaworks/boyan/pkg/memory"

func (d *Deriver) available() error {
	if d == nil || d.invoker == nil {
		return memory.PermanentError(memory.CodeModelFailure, "deriver is unavailable", nil)
	}

	return nil
}
