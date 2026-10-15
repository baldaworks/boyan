package semantic

import "errors"

// Reason is a stable, text-free semantic failure classification.
type Reason string

const (
	ReasonInvalidInput       Reason = "invalid_input"
	ReasonInputLimit         Reason = "input_limit"
	ReasonUnavailable        Reason = "unavailable"
	ReasonDeadline           Reason = "deadline"
	ReasonResponseLimit      Reason = "response_limit"
	ReasonInvalidResponse    Reason = "invalid_response"
	ReasonModelMismatch      Reason = "model_mismatch"
	ReasonDimensionMismatch  Reason = "dimension_mismatch"
	ReasonProjectionCapacity Reason = "projection_capacity"
)

// Error deliberately retains no input, provider, endpoint, or upstream prose.
type Error struct {
	Reason Reason
}

func (failure *Error) Error() string {
	if failure == nil {
		return ""
	}

	if !failure.Reason.Valid() {
		return string(ReasonInvalidResponse)
	}

	return string(failure.Reason)
}

// Classify returns the stable safe reason carried by err.
func Classify(err error) (Reason, bool) {
	var failure *Error
	if !errors.As(err, &failure) || failure == nil || !failure.Reason.Valid() {
		return "", false
	}

	return failure.Reason, true
}

// Valid reports whether reason is part of the bounded public diagnostic set.
func (reason Reason) Valid() bool {
	switch reason {
	case ReasonInvalidInput, ReasonInputLimit, ReasonUnavailable, ReasonDeadline,
		ReasonResponseLimit, ReasonInvalidResponse, ReasonModelMismatch,
		ReasonDimensionMismatch, ReasonProjectionCapacity:
		return true
	default:
		return false
	}
}

func newError(reason Reason) error {
	return &Error{Reason: reason}
}
