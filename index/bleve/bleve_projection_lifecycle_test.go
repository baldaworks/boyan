package bleve_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	bleve "github.com/baldaworks/boyan/index/bleve"
	boyan "github.com/baldaworks/boyan/pkg/memory"
)

func TestBleveProjectionRejectsOperationsAfterClose(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	projection, err := bleve.Open(root)
	requireNoError(t, err, "Open() error = %v")
	requireNoError(t, projection.Close(), "Close() error = %v")

	_, err = projection.SearchRecall(context.Background(), boyan.RecallRequest{
		Scope: boyan.Scope{Key: "test:closed", Kind: boyan.ScopeKindPersonal},
		Query: "memory",
		Limit: 1,
	})
	assertUnavailable(t, err)

	if _, err = projection.NewGeneration("after-close"); err == nil {
		t.Fatal("NewGeneration() error = nil after Close")
	}
	if _, statErr := os.Stat(filepath.Join(root, "generations", "after-close")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("generation path stat error = %v, want not-exist", statErr)
	}
}

func assertUnavailable(t *testing.T, err error) {
	t.Helper()
	code, class, ok := boyan.ClassifyError(err)
	if !ok || code != boyan.CodeUnavailable || class != boyan.ErrorClassRetryable {
		t.Fatalf("ClassifyError(%v) = %q, %q, %v; want unavailable, retryable, true", err, code, class, ok)
	}
}
