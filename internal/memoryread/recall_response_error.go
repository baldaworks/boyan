package memoryread

import memory "github.com/baldaworks/boyan/pkg/memory"

func skipStaleRecallError(err error) bool {
	code, _, ok := memory.ClassifyError(err)

	return ok && (code == memory.CodeNotFound || code == memory.CodeForgotten)
}
