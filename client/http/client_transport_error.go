package clienthttp

import (
	"context"
	"errors"
	"net"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func transportError(message string, err error) error {
	var netErr net.Error
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.As(err, &netErr) && netErr.Timeout() {
		return memory.RetryableError(memory.CodeTimeout, message, err)
	}

	return memory.RetryableError(memory.CodeUnavailable, message, err)
}
