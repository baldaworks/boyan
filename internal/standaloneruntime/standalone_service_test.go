package standaloneruntime //nolint:testpackage // Exercises shared private test fixtures within the package.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRuntimeServesHealthAndStops(t *testing.T) {
	t.Parallel()

	cfg := testStandaloneConfig(t)

	runtime, err := New(cfg)
	requireNoError(t, err, "New() error = %v")

	if runtime.Ready() {
		t.Fatal("runtime.Ready() = true, want false before Start")
	}

	preStart := httptest.NewRecorder()
	preStartRequest := httptest.NewRequest(http.MethodGet, "http://boyan/readyz", nil)
	runtime.Handler().ServeHTTP(preStart, preStartRequest)

	if preStart.Code != http.StatusServiceUnavailable {
		t.Fatalf(
			"pre-start readiness status = %d, want %d",
			preStart.Code,
			http.StatusServiceUnavailable,
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	requireNoError(t, runtime.Start(ctx), "runtime.Start() error = %v")

	if !runtime.Ready() {
		t.Fatal("runtime.Ready() = false, want true after Start")
	}

	if got := runtime.Addr(); got == cfg.HTTP.Listen {
		t.Fatalf("runtime.Addr() = %q, want bound listener address", got)
	}

	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()

		requireNoError(t, runtime.Stop(stopCtx), "runtime.Stop() error = %v")
	})

	client := &http.Client{Timeout: 2 * time.Second}
	for _, path := range []string{"/livez", "/readyz"} {
		response, err := client.Get("http://" + runtime.Addr() + path)
		if err != nil {
			t.Fatalf("GET %s error = %v", path, err)
		}

		response.Body.Close()

		if response.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", path, response.StatusCode)
		}
	}

	assertRuntimeStopsAfterCanceledWait(t, runtime)
}

func assertRuntimeStopsAfterCanceledWait(t *testing.T, runtime *Runtime) {
	t.Helper()

	waitCtx, waitCancel := context.WithCancel(context.Background())
	waitCancel()

	if err := runtime.Wait(waitCtx); err == nil {
		t.Fatal("runtime.Wait(canceled) error = nil, want context cancellation")
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()

	requireNoError(t, runtime.Stop(stopCtx), "runtime.Stop() error = %v")

	if runtime.Ready() {
		t.Fatal("runtime.Ready() = true, want false after Stop")
	}

	requireNoError(t, runtime.Wait(context.Background()), "runtime.Wait() after Stop error = %v")
}
