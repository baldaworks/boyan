package badger

import (
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func canonicalStoreError(operation string, err error) error {
	if errors.Is(err, badger.ErrTxnTooBig) {
		return memory.PermanentError(memory.CodeLimitExceeded, operation, err)
	}
	if errors.Is(err, badger.ErrConflict) {
		return memory.RetryableError(memory.CodeConflict, operation, err)
	}
	var memoryError *memory.Error
	if errors.As(err, &memoryError) {
		return err
	}
	return memory.RetryableError(memory.CodeStoreFailure, operation, err)
}
