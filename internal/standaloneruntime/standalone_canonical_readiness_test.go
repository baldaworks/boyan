//go:build integration

package standaloneruntime //nolint:testpackage // Exercises shutdown and durable readiness gates.

import (
	"context"
	"testing"
	"time"
)

func TestSQLRuntimeReadinessBoundedByShutdown(t *testing.T) {
	forEachSQLRuntimeConfig(t, func(t *testing.T, cfg Config) {
		runtime, err := New(cfg)
		requireNoError(t, err, "new: %v")
		t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
		requireNoError(t, runtime.Start(t.Context()), "start: %v")

		if !runtime.Ready() {
			t.Fatal("healthy control is not ready")
		}
		// This is the same cancellation invoked by beginStop. Even before the
		// closing flag changes, a canceled shutdown context must prevent ready.
		runtime.service.cancelOperations()

		started := time.Now()
		if runtime.Ready() || time.Since(started) > 500*time.Millisecond {
			t.Fatal("readiness ignored shutdown cancellation")
		}

		requireNoError(t, runtime.Stop(t.Context()), "drain: %v")
	})
}

func TestSQLRuntimeReadinessRespectsDurableGates(t *testing.T) {
	forEachSQLRuntimeConfig(t, func(t *testing.T, cfg Config) {
		runtime, err := New(cfg)
		requireNoError(t, err, "new: %v")
		t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
		requireNoError(t, runtime.Start(t.Context()), "start: %v")

		if !runtime.Ready() {
			t.Fatal("healthy control is not ready")
		}

		p := nativeRuntimeProbe(t, cfg)
		requireNoError(t, p.exec("DELETE FROM "+p.table("source_privacy_ready")), "remove marker: %v")

		if runtime.Ready() {
			t.Fatal("health probe bypassed durable serving gate")
		}
	})
}
