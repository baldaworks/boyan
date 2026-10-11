package structuredderive

import memory "github.com/baldaworks/boyan/pkg/memory"

func classifyDeriverError(err error) error {
	if _, _, ok := memory.ClassifyError(err); ok {
		return err
	}

	return memory.RetryableError(memory.CodeModelFailure, "structured derivation failed", nil)
}
