package structuredderive

import "context"

// StructuredInvocation is the neutral request passed to a host-specific model
// invoker. The typed deriver owns operation identity and output contracts.
type StructuredInvocation struct {
	OperationID  string
	Stage        string
	Instruction  string
	InputJSON    []byte
	OutputSchema string
}

// StructuredInvoker executes one structured derivation request.
type StructuredInvoker interface {
	Invoke(ctx context.Context, request StructuredInvocation) ([]byte, error)
	Close(ctx context.Context) error
}
