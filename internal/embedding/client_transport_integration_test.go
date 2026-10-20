//go:build integration

package embedding_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/embedding"
)

func TestOpenAIProviderPropagatesCallerCancellation(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		<-release
	}))
	t.Cleanup(server.Close)
	provider := newTestProvider(t, server.URL, 2)

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)

	go func() {
		_, err := provider.Embed(ctx, []string{"input-secret"})
		result <- err
	}()

	<-started
	cancel()

	err := <-result

	close(release)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Embed() error = %v, want context.Canceled", err)
	}
}

func TestOpenAIProviderPropagatesCallerDeadline(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-release
	}))
	t.Cleanup(server.Close)
	provider := newTestProvider(t, server.URL, 2)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_, err := provider.Embed(ctx, []string{"input-secret"})

	close(release)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Embed() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestOpenAIProviderAppliesTenSecondCallDeadline(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-release
	}))
	t.Cleanup(server.Close)
	provider := newTestProvider(t, server.URL, 2)

	started := time.Now()
	_, err := provider.Embed(context.Background(), []string{"input-secret"})

	close(release)

	elapsed := time.Since(started)

	requireFailureCode(t, err, embedding.CodeDeadline)

	if elapsed < 9*time.Second || elapsed > 12*time.Second {
		t.Fatalf("Embed() elapsed = %v, want local deadline near 10s", elapsed)
	}
}

func TestOpenAIProviderDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()

	var redirected atomic.Int32

	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		redirected.Add(1)

		_, _ = writer.Write([]byte(validResponse()))
	}))
	t.Cleanup(target.Close)

	origin := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(origin.Close)

	provider := newTestProvider(t, origin.URL, 2)
	_, err := provider.Embed(context.Background(), []string{"input-secret"})
	requireFailureCode(t, err, embedding.CodeUnavailable)

	if got := redirected.Load(); got != 0 {
		t.Fatalf("redirected requests = %d, want 0", got)
	}
}

func TestOpenAIProviderNeverRetries(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	provider := newTestProvider(t, server.URL, 2)

	_, err := provider.Embed(context.Background(), []string{"input-secret"})
	requireFailureCode(t, err, embedding.CodeUnavailable)

	if got := calls.Load(); got != 1 {
		t.Fatalf("requests = %d, want exactly 1", got)
	}
}

func TestOpenAIProviderOwnsTransportAfterConstruction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(validResponse()))
	}))
	t.Cleanup(server.Close)

	provider := newTestProvider(t, server.URL, 2)
	original := http.DefaultTransport

	defaultTransport, ok := original.(*http.Transport)
	if !ok {
		t.Fatalf("http.DefaultTransport = %T, want *http.Transport", original)
	}

	originalProxy := defaultTransport.Proxy

	var calls atomic.Int32

	defaultTransport.Proxy = func(*http.Request) (*url.URL, error) {
		calls.Add(1)

		return nil, errors.New("mutated default proxy")
	}
	http.DefaultTransport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)

		return nil, errors.New("mutated default transport")
	})

	t.Cleanup(func() {
		defaultTransport.Proxy = originalProxy
		http.DefaultTransport = original
	})

	if _, err := provider.Embed(context.Background(), []string{"input-secret"}); err != nil {
		t.Fatalf("Embed() error = %v", err)
	}

	if got := calls.Load(); got != 0 {
		t.Fatalf("mutated http.DefaultTransport calls = %d, want 0", got)
	}
}

func TestOpenAIProviderIgnoresReplacedDefaultTransportAtConstruction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(validResponse()))
	}))
	t.Cleanup(server.Close)

	original := http.DefaultTransport

	var calls atomic.Int32

	http.DefaultTransport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)

		return nil, errors.New("hostile default transport")
	})

	t.Cleanup(func() { http.DefaultTransport = original })

	provider := newTestProvider(t, server.URL, 2)
	if _, err := provider.Embed(context.Background(), []string{"input-secret"}); err != nil {
		t.Fatalf("Embed() error = %v", err)
	}

	if got := calls.Load(); got != 0 {
		t.Fatalf("hostile http.DefaultTransport calls = %d, want 0", got)
	}
}

func TestOpenAIProviderIgnoresMutatedDefaultTransportAtConstruction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(validResponse()))
	}))
	t.Cleanup(server.Close)

	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Fatalf("http.DefaultTransport = %T, want *http.Transport", http.DefaultTransport)
	}

	originalProxy := defaultTransport.Proxy

	var calls atomic.Int32

	defaultTransport.Proxy = func(*http.Request) (*url.URL, error) {
		calls.Add(1)

		return nil, errors.New("hostile default proxy")
	}

	t.Cleanup(func() { defaultTransport.Proxy = originalProxy })

	provider := newTestProvider(t, server.URL, 2)
	if _, err := provider.Embed(context.Background(), []string{"input-secret"}); err != nil {
		t.Fatalf("Embed() error = %v", err)
	}

	if got := calls.Load(); got != 0 {
		t.Fatalf("hostile http.DefaultTransport proxy calls = %d, want 0", got)
	}
}

func TestOpenAIProviderClassifiesTransportFailureWithoutDisclosure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	endpoint := server.URL
	server.Close()

	provider := newTestProvider(t, endpoint, 2)

	_, err := provider.Embed(context.Background(), []string{"input-secret"})
	requireFailureCode(t, err, embedding.CodeUnavailable)
	requireRedacted(t, err, endpoint, "input-secret", "bearer-secret")
}

func TestOpenAIProviderLimitsConcurrentCallsToFour(t *testing.T) {
	t.Parallel()

	const callCount = 12

	started := make(chan struct{}, callCount)
	release := make(chan struct{})

	var (
		active  atomic.Int32
		maximum atomic.Int32
	)

	server := httptest.NewServer(concurrencyHandler(started, release, &active, &maximum))
	t.Cleanup(server.Close)
	provider := newTestProvider(t, server.URL, 2)

	errorsByCall := make(chan error, callCount)

	var wait sync.WaitGroup
	for range callCount {
		wait.Add(1)
		go func() {
			defer wait.Done()

			_, err := provider.Embed(context.Background(), []string{"text"})
			errorsByCall <- err
		}()
	}

	for range 4 {
		<-started
	}

	time.Sleep(50 * time.Millisecond)

	if got := maximum.Load(); got > 4 {
		close(release)
		wait.Wait()
		t.Fatalf("maximum concurrent requests = %d, want at most 4", got)
	}

	close(release)
	wait.Wait()
	close(errorsByCall)

	for err := range errorsByCall {
		if err != nil {
			t.Fatalf("Embed() error = %v", err)
		}
	}

	if got := maximum.Load(); got != 4 {
		t.Fatalf("maximum concurrent requests = %d, want 4", got)
	}
}

func concurrencyHandler(
	started chan<- struct{},
	release <-chan struct{},
	active *atomic.Int32,
	maximum *atomic.Int32,
) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		current := active.Add(1)

		observed := maximum.Load()
		for current > observed && !maximum.CompareAndSwap(observed, current) {
			observed = maximum.Load()
		}

		started <- struct{}{}

		<-release
		active.Add(-1)

		_, _ = writer.Write([]byte(validResponse()))
	})
}

func TestOpenAIProviderHonorsDeadlineWhileWaitingForSlot(t *testing.T) {
	t.Parallel()

	started := make(chan struct{}, 5)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		started <- struct{}{}

		<-release

		_, _ = writer.Write([]byte(validResponse()))
	}))
	t.Cleanup(server.Close)
	provider := newTestProvider(t, server.URL, 2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wait sync.WaitGroup
	for range 4 {
		wait.Add(1)
		go func() {
			defer wait.Done()

			_, _ = provider.Embed(ctx, []string{"text"})
		}()
	}

	for range 4 {
		<-started
	}

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer waitCancel()

	_, err := provider.Embed(waitCtx, []string{"waiting"})
	if !errors.Is(err, context.DeadlineExceeded) {
		close(release)
		wait.Wait()
		t.Fatalf("Embed() waiting error = %v, want context.DeadlineExceeded", err)
	}

	select {
	case <-started:
		close(release)
		wait.Wait()
		t.Fatal("fifth request reached network before a slot was released")
	default:
	}

	close(release)
	wait.Wait()
}
