package portableapp

import "context"

// Lifecycle is implemented by resources that need ordered startup and
// shutdown. A nil Lifecycle is never accepted by Runtime.
type Lifecycle interface {
	Start(ctx context.Context) error
	Close(ctx context.Context) error
}
