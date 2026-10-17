package standaloneruntime //nolint:testpackage // Exercises applier within the package.

import (
	"context"
	"testing"
)

func TestRuntimeConstructedStop(t *testing.T) {
	cfg := testStandaloneConfig(t)

	first, err := New(cfg)
	requireNoError(t, err, "%v")

	defer first.service.store.Close()
	defer first.service.index.Close()

	requireNoError(t, first.Stop(context.Background()), "%v")

	second, err := New(cfg)
	requireNoError(t, err, "reopen after Stop: %v")

	defer second.service.store.Close()
	defer second.service.index.Close()
}

func TestRuntimeWaitAfterConstructedStop(t *testing.T) {
	runtime, err := New(testStandaloneConfig(t))
	requireNoError(t, err, "%v")

	defer runtime.service.store.Close()
	defer runtime.service.index.Close()

	requireNoError(t, runtime.Stop(context.Background()), "%v")

	for range 3 {
		if err := runtime.Wait(context.Background()); err != nil {
			t.Errorf("Wait after terminal Stop: %v", err)
		}
	}
}
