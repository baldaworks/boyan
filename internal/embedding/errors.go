package embedding

import "errors"

// ErrorCode is a stable embedding failure classification.
type ErrorCode string

const (
	CodeInvalidInput       ErrorCode = "invalid_input"
	CodeInputLimit         ErrorCode = "input_limit"
	CodeUnavailable        ErrorCode = "unavailable"
	CodeDeadline           ErrorCode = "deadline"
	CodeResponseLimit      ErrorCode = "response_limit"
	CodeInvalidResponse    ErrorCode = "invalid_response"
	CodeModelMismatch      ErrorCode = "model_mismatch"
	CodeDimensionMismatch  ErrorCode = "dimension_mismatch"
	CodeProjectionCapacity ErrorCode = "projection_capacity"
)

// Error carries only a stable safe code. It never retains provider data.
type Error struct {
	Code ErrorCode
}

func (failure *Error) Error() string {
	if failure == nil {
		return ""
	}

	return string(failure.Code)
}

// Classify returns the stable embedding failure code carried by err.
func Classify(err error) (ErrorCode, bool) {
	var failure *Error
	if !errors.As(err, &failure) || failure == nil || failure.Code == "" {
		return "", false
	}

	return failure.Code, true
}

func newError(code ErrorCode) error {
	return &Error{Code: code}
}
