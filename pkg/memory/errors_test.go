package memory

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrorClassificationSurvivesWrapping(t *testing.T) {
	t.Parallel()

	cause := errors.New("provider unavailable")
	err := fmt.Errorf("sync turn: %w", RetryableError(CodeUnavailable, "memory provider unavailable", cause))
	code, class, ok := ClassifyError(err)
	if !ok || code != CodeUnavailable || class != ErrorClassRetryable {
		t.Fatalf("ClassifyError() = %q, %q, %v", code, class, ok)
	}
	if !IsRetryable(err) {
		t.Fatal("IsRetryable() = false, want true")
	}
	if !errors.Is(err, cause) {
		t.Fatal("errors.Is() = false, want wrapped cause")
	}
}
