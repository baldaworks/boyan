package sqlite

import (
	"context"
	"errors"
	"math"

	"github.com/baldaworks/boyan/pkg/memory"
	driver "modernc.org/sqlite"
)

type lexicalBuffer struct {
	applier *SQLiteCanonicalApplier
	build   string
	inputs  []lexicalInput
	bytes   uint64
}

func (buffer *lexicalBuffer) add(ctx context.Context, input lexicalInput) error {
	size, err := input.encodedBytes(buffer.build)
	if err != nil {
		return err
	}

	if len(buffer.inputs) == lexicalPageRecords || size > lexicalPageBytes-buffer.bytes {
		if err = buffer.flush(ctx); err != nil {
			return err
		}
	}

	if size > lexicalPageBytes {
		// The scheduling target never rejects an otherwise valid canonical record.
		return buffer.applier.writeInputs(ctx, buffer.build, []lexicalInput{input})
	}

	buffer.inputs = append(buffer.inputs, input)
	buffer.bytes += size

	return nil
}

func (buffer *lexicalBuffer) flush(ctx context.Context) error {
	if len(buffer.inputs) == 0 {
		return ctx.Err()
	}

	if err := buffer.applier.writeInputs(ctx, buffer.build, buffer.inputs); err != nil {
		return err
	}
	// Drop retained strings and membership arrays, including after large writes.
	buffer.inputs = nil
	buffer.bytes = 0

	return nil
}

type inputByteCounter struct {
	bytes uint64
	err   error
}

func (counter *inputByteCounter) add(n int) {
	if counter.err == nil {
		counter.bytes, counter.err = checkedInputBytes(counter.bytes, uint64(n))
	}
}

func checkedInputBytes(total, added uint64) (uint64, error) {
	if added > math.MaxUint64-total {
		return 0, memory.PermanentError(memory.CodeStoreFailure, "sqlite input accounting overflow", nil)
	}

	return total + added, nil
}

func lexicalStorageError(err error) error {
	if err == nil || errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrClosed) {
		return err
	}

	var classified *memory.Error
	if errors.As(err, &classified) {
		return err
	}

	var sqlError *driver.Error
	if errors.As(err, &sqlError) && (sqlError.Code()&255 == 5 || sqlError.Code()&255 == 6) {
		return memory.RetryableError(memory.CodeConflict, "sqlite lexical write conflicts", nil)
	}
	// Driver prose can include SQL parameters or private identities. Only a fixed
	// classified storage failure crosses this projection adapter boundary.
	return memory.PermanentError(memory.CodeStoreFailure, "sqlite lexical state could not be repaired", nil)
}
