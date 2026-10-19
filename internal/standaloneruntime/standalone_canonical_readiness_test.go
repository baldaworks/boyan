package standaloneruntime //nolint:testpackage // Exercises real admission and durable readiness gates.

import (
	"context"
	"testing"
	"time"

	"github.com/baldaworks/boyan/store/archive"
)

type runtimeBlockedBackup struct {
	entered chan struct{}
	ctx     context.Context
}

func (writer runtimeBlockedBackup) Write([]byte) (int, error) {
	close(writer.entered)
	<-writer.ctx.Done()

	return 0, writer.ctx.Err()
}

func TestSQLRuntimeReadinessBoundedByShutdown(t *testing.T) {
	forEachSQLRuntimeConfig(t, func(t *testing.T, cfg Config) {
		runtime, err := New(cfg)
		requireNoError(t, err, "new: %v")
		t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
		requireNoError(t, runtime.Start(t.Context()), "start: %v")

		entered := make(chan struct{})
		done := make(chan error, 1)

		go func() {
			_, err := runtime.service.store.(archive.CanonicalBackupWriter).WriteCanonicalBackup(t.Context(),
				runtimeBlockedBackup{entered: entered, ctx: runtime.service.shutdownContext}, 0)
			done <- err
		}()

		<-entered
		// The backup owns real native admission, so readiness must time out
		// without waiting indefinitely for this callback to return.
		started := time.Now()
		if runtime.Ready() || time.Since(started) > 2*time.Second {
			t.Fatal("native readiness was not bounded while admission was occupied")
		}
		// This is the same cancellation invoked by beginStop. Even before the
		// closing flag changes, a canceled shutdown context must prevent ready.
		runtime.service.cancelOperations()

		started = time.Now()
		if runtime.Ready() || time.Since(started) > 500*time.Millisecond {
			t.Fatal("readiness ignored shutdown cancellation")
		}

		if err := <-done; err == nil {
			t.Fatal("canceled native backup acknowledged success")
		}

		requireNoError(t, runtime.Stop(t.Context()), "drain: %v")
	})
}

func TestSQLRuntimeReadinessRespectsDurableGates(t *testing.T) {
	for _, mode := range []string{"privacy", "loading"} {
		t.Run(mode, func(t *testing.T) {
			forEachSQLRuntimeConfig(t, func(t *testing.T, cfg Config) {
				runtime, err := New(cfg)
				requireNoError(t, err, "new: %v")
				t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
				requireNoError(t, runtime.Start(t.Context()), "start: %v")

				if !runtime.Ready() {
					t.Fatal("healthy control is not ready")
				}

				p := nativeRuntimeProbe(t, cfg)
				if mode == "privacy" {
					requireNoError(t, p.exec("DELETE FROM "+p.table("source_privacy_ready")), "remove marker: %v")
				} else {
					requireNoError(t, p.exec("INSERT INTO "+p.table("restore_control")+"(singleton,control) VALUES(1,$1)",
						[]byte(`{"version":1,"phase":"loading"}`)), "loading fence: %v")
				}

				if runtime.Ready() {
					t.Fatal("health probe bypassed durable serving gate")
				}
			})
		})
	}
}
