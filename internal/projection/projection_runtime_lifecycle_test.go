package projection //nolint:testpackage // Exercises active within the package.

import (
	"context"
	"testing"
	"time"
)

func TestProjectionCloseHonorsContext(t *testing.T) {
	runtime := &ProjectionRuntime{}

	runtime.active.Add(1)
	defer runtime.active.Done()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- runtime.Close(ctx) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Close reported drained while work active")
		}
	case <-time.After(time.Second):
		t.Fatal("Close ignored context deadline")
	}
}
