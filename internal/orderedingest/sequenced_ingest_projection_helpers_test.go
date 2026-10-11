package orderedingest //nolint:testpackage // Exercises shared private test fixtures within the package.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

type flakyProjectionSyncer struct {
	mu                sync.Mutex
	failuresRemaining int
	attempts          int
	synced            chan struct{}
}

func (s *flakyProjectionSyncer) Sync(context.Context, boyan.Scope) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.attempts++
	select {
	case s.synced <- struct{}{}:
	default:
	}

	if s.failuresRemaining > 0 {
		s.failuresRemaining--

		return errors.New("projection lag")
	}

	return nil
}

func hasCodeClass(err error, code boyan.ErrorCode, class boyan.ErrorClass) bool {
	gotCode, gotClass, ok := boyan.ClassifyError(err)

	return ok && gotCode == code && gotClass == class
}

func waitForProjectionSync(t *testing.T, syncer *flakyProjectionSyncer, want int) {
	t.Helper()

	deadline := time.After(500 * time.Millisecond)

	for {
		syncer.mu.Lock()
		got := syncer.attempts
		syncer.mu.Unlock()

		if got >= want {
			return
		}

		select {
		case <-deadline:
			t.Fatalf("projection sync attempts = %d, want >= %d", got, want)
		case <-time.After(10 * time.Millisecond):
		}
	}
}
