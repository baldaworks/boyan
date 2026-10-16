package semantic

import "errors"

var (
	ErrInvalid  = errors.New("invalid semantic projection")
	ErrCapacity = errors.New("semantic projection capacity exceeded")
	ErrConflict = errors.New("semantic projection publication conflict")
	ErrCorrupt  = errors.New("corrupt semantic projection")
	ErrClosed   = errors.New("semantic projection closed")
)
