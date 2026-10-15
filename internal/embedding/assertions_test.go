package embedding_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/internal/embedding"
)

func requireFailureCode(t *testing.T, err error, want embedding.ErrorCode) {
	t.Helper()

	got, ok := embedding.Classify(err)
	if !ok || got != want {
		t.Fatalf("failure = (%q, %t), want (%q, true); error = %v", got, ok, want, err)
	}
}

func requireRedacted(t *testing.T, err error, secrets ...string) {
	t.Helper()

	for _, secret := range secrets {
		if secret != "" && strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaked protected value %q: %v", secret, err)
		}
	}

	var failure *embedding.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error type = %T, want *embedding.Error", err)
	}
}
