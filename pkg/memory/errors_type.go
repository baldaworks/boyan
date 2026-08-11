package memory

import (
	"fmt"
	"strings"
)

// Error is a stable, classifiable Boyan failure.
type Error struct {
	Code    ErrorCode
	Class   ErrorClass
	Message string
	Cause   error
}

// Error returns a diagnostic message without changing the stable code.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = string(e.Code)
	}
	if e.Cause == nil {
		return message
	}
	return fmt.Sprintf("%s: %v", message, e.Cause)
}

// Unwrap returns the underlying provider or validation failure.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func newError(code ErrorCode, class ErrorClass, message string, cause error) error {
	return &Error{Code: code, Class: class, Message: strings.TrimSpace(message), Cause: cause}
}
