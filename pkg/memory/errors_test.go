package memory_test

import memory "github.com/baldaworks/boyan/pkg/memory"

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrorClassificationSurvivesWrapping(t *testing.T) {
	t.Parallel()

	cause := errors.New("provider unavailable")
	err := fmt.Errorf(
		"sync turn: %w",
		memory.RetryableError(memory.CodeUnavailable, "memory provider unavailable", cause),
	)

	code, class, ok := memory.ClassifyError(err)
	if !ok || code != memory.CodeUnavailable || class != memory.ErrorClassRetryable {
		t.Fatalf("ClassifyError() = %q, %q, %v", code, class, ok)
	}

	if !memory.IsRetryable(err) {
		t.Fatal("IsRetryable() = false, want true")
	}

	if !errors.Is(err, cause) {
		t.Fatal("errors.Is() = false, want wrapped cause")
	}
}
