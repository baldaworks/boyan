//go:build integration

package canonicalcore_test

import (
	"context"
	"os"
	"testing"
	"time"

	index "github.com/baldaworks/boyan/index/sqlite"
	read "github.com/baldaworks/boyan/internal/memoryread"
	runtime "github.com/baldaworks/boyan/internal/projection"
	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestNativeAsOfFiltersCurrentHeadAfterSupersession(t *testing.T) {
	t.Parallel()
	runCoreAuthorities(t, func(t *testing.T, factory coreFactory) {
		reader, processor, turn, _, store := nativeFlow(t, factory)
		replacement,
			err := m.NewTurn(turn.Scope,
			turn.Conversation,
			"turn-osh",
			turn.CompletedAt.Add(2*time.Hour),
			"Я живу в Оше",
			"OK")
		requireNoError(t, err, "replacement: %v")
		_, err = processor.ProcessTurn(context.Background(), replacement, testProcessorDerivation())
		requireNoError(t, err, "process: %v")
		root := t.TempDir()
		requireNoError(t, os.Chmod(root, 0o700), "private state: %v")
		projection, err := index.Open(t.Context(), root, index.Options{})
		requireNoError(t, err, "projection: %v")
		t.Cleanup(func() { requireNoError(t, projection.Close(), "close: %v") })

		applier, err := index.NewSQLiteCanonicalApplier(projection, reader, store)
		requireNoError(t, err, "applier: %v")
		runner, err := runtime.NewProjectionRuntime(store, store, applier, "sqlite", 128)
		requireNoError(t, err, "runtime: %v")
		requireNoError(t, runner.Sync(context.Background(), turn.Scope), "sync: %v")

		for _, adapter := range []m.RecallProjection{nil, projection} {
			service, serviceErr := read.NewRecallService(reader, adapter)
			requireNoError(t, serviceErr, "service: %v")
			assertNativeAsOf(t, service, turn, replacement)
		}
	})
}

func assertNativeAsOf(t *testing.T, service *read.RecallService, first, second m.Turn) {
	t.Helper()

	between := first.CompletedAt.Add(time.Hour)
	for _, tc := range []struct {
		at   time.Time
		want int
	}{
		{first.CompletedAt, 0}, {between, 0}, {second.CompletedAt, 1},
	} {
		request := m.RecallRequest{Scope: first.Scope, Query: "живу", Limit: 10, AsOf: &tc.at}
		response, err := service.Search(context.Background(), request)
		requireNoError(t, err, "search: %v")
		requireNoError(t, response.Validate(request), "response: %v")

		if len(response.Results) != tc.want {
			t.Fatalf("as_of %s: %+v", tc.at, response.Results)
		}

		if tc.want == 1 && response.Results[0].Text != second.Messages[0].Text {
			t.Fatalf("wrong current head: %+v",
				response.Results)
		}
	}

	request := m.RecallRequest{Scope: m.Scope{Key: "test:other", Kind: m.ScopeKindPersonal}, Query: "живу", Limit: 10}
	response, err := service.Search(context.Background(), request)
	requireNoError(t, err, "foreign search: %v")

	if len(response.Results) != 0 {
		t.Fatal("other scope exposed current head")
	}
}
