package canonicalsql

import (
	"context"
	"sync"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Owner serializes one pinned connection. Transaction helpers must use the
// admitted Tx and context, never call back into an owner while holding it.
type Owner struct {
	connection   *Connection
	closeNative  func() error
	gate         chan struct{}
	stopping     chan struct{}
	closed       chan struct{}
	mu           sync.Mutex
	closing      bool
	poisoned     bool
	activeCancel context.CancelFunc
	closeOnce    sync.Once
	closeErr     error
}

type admissionKey struct{}

func newOwner(connection *Connection, closeNative func() error) *Owner {
	o := &Owner{
		connection: connection, closeNative: closeNative,
		gate: make(chan struct{}, 1), stopping: make(chan struct{}), closed: make(chan struct{}),
	}
	o.gate <- struct{}{}

	return o
}

func ownerUnavailable() error {
	return memory.RetryableError(memory.CodeUnavailable, "canonical SQL owner unavailable", nil)
}

// Update runs one writer transaction. Its context is canceled on Close, and
// any error rolls back before admission is released.
func (o *Owner) Update(ctx context.Context, fn func(context.Context, *Tx) error) error {
	return o.run(ctx, true, fn)
}

// View holds a consistent snapshot under the same lifecycle gate as writes.
func (o *Owner) View(ctx context.Context, fn func(context.Context, *Tx) error) error {
	return o.run(ctx, false, fn)
}

// RestorePreflight checks durable control before application startup. Opening
// a fenced store remains possible for the controlled recovery added later.
func (o *Owner) RestorePreflight(ctx context.Context) error {
	return o.View(ctx, func(admitted context.Context, tx *Tx) error { return tx.CheckIntegrity(admitted) })
}

// Close rejects waiting/new work, cancels active work, and waits for rollback
// before releasing the native connection and exclusive ownership. It is safe
// to call concurrently; admitted callbacks must honor their supplied context.
func (o *Owner) Close() error {
	if o == nil {
		return nil
	}

	o.closeOnce.Do(func() {
		o.mu.Lock()
		o.closing = true
		close(o.stopping)

		if o.activeCancel != nil {
			o.activeCancel()
		}
		o.mu.Unlock()

		<-o.gate
		o.closeErr = o.closeNative()
		close(o.closed)
	})
	<-o.closed

	return o.closeErr
}

func (o *Owner) run(ctx context.Context, write bool, fn func(context.Context, *Tx) error) error {
	if ctx == nil || fn == nil {
		return invalid("invalid canonical SQL admission")
	}

	admitted, release, err := o.admit(ctx)
	if err != nil {
		return err
	}
	defer release()

	if err := o.checkPostgresOwnership(admitted); err != nil {
		return err
	}

	defer o.fenceBrokenPostgres()

	callback := func(tx *Tx) error { return fn(admitted, tx) }
	if write {
		return o.connection.Update(admitted, callback)
	}

	return o.connection.View(admitted, callback)
}

func (o *Owner) admit(ctx context.Context) (context.Context, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	if o == nil {
		return nil, nil, ownerUnavailable()
	}

	if ctx.Value(admissionKey{}) == o {
		return nil, nil, invalid("recursive canonical SQL admission")
	}

	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case <-o.stopping:
		return nil, nil, ownerUnavailable()
	case <-o.gate:
	}

	o.mu.Lock()
	if o.closing || o.poisoned {
		o.mu.Unlock()

		o.gate <- struct{}{}

		return nil, nil, ownerUnavailable()
	}

	admitted, cancel := context.WithCancel(context.WithValue(ctx, admissionKey{}, o))
	o.activeCancel = cancel
	o.mu.Unlock()

	release := func() {
		cancel()
		o.mu.Lock()
		o.activeCancel = nil
		o.mu.Unlock()

		o.gate <- struct{}{}
	}

	return admitted, release, nil
}
