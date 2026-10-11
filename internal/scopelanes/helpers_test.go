package scopelanes //nolint:testpackage // Exercises mu within the package.

import (
	"testing"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

func waitForLanePending(t *testing.T, executor *Executor, scope boyan.Scope, want int) {
	t.Helper()

	key := string(scope.Kind) + "\x00" + scope.Key

	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		executor.mu.Lock()
		lane := executor.lanes[key]

		got := 0
		if lane != nil {
			got = len(lane.pending)
		}
		executor.mu.Unlock()

		if got >= want {
			return
		}

		time.Sleep(time.Millisecond)
	}

	t.Fatalf("lane pending count did not reach %d", want)
}
