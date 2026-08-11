package memory

import "errors"

// RetryableError creates a retryable failure with a stable code.
func RetryableError(code ErrorCode, message string, cause error) error {
	return newError(code, ErrorClassRetryable, message, cause)
}

// PermanentError creates a non-retryable failure with a stable code.
func PermanentError(code ErrorCode, message string, cause error) error {
	return newError(code, ErrorClassPermanent, message, cause)
}

// ClassifyError returns the stable code and retry class carried by err.
func ClassifyError(err error) (ErrorCode, ErrorClass, bool) {
	var target *Error
	if !errors.As(err, &target) || target == nil || target.Code == "" || target.Class == "" {
		return "", "", false
	}
	return target.Code, target.Class, true
}

// IsRetryable reports whether err explicitly permits retry.
func IsRetryable(err error) bool {
	_, class, ok := ClassifyError(err)
	return ok && class == ErrorClassRetryable
}
