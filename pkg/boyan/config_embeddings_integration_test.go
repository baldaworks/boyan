//go:build integration

package boyan_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	boyan "github.com/baldaworks/boyan/pkg/boyan"
)

func TestNewDisabledEmbeddingsIgnoreInvalidFieldsAndCreateNoSemanticState(t *testing.T) {
	config := testHostConfig(t)
	config.Embeddings = boyan.EmbeddingsConfig{
		Endpoint:      "not a URL",
		Dimensions:    -1,
		APIKeyEnv:     "ABSENT_BOYAN_DISABLED_EMBEDDING_SECRET",
		FailurePolicy: "unsupported",
	}

	host, err := boyan.New(context.Background(), config)
	requireNoError(t, err, "New() error = %v")
	t.Cleanup(func() { _ = host.Close() })

	if _, statErr := os.Stat(filepath.Join(config.StateDir, "embeddings")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("semantic state stat error = %v, want not-exist", statErr)
	}
}

func TestNewEnabledConfigOpensPrivateSemanticStateWithoutPersistingCredential(t *testing.T) {
	const (
		name   = "BOYAN_TEST_VALID_EMBEDDING_CREDENTIAL"
		secret = "operator-secret-not-for-storage"
	)
	t.Setenv(name, secret)

	config := testHostConfig(t)
	config.Embeddings = validEmbeddingsConfig()
	config.Embeddings.APIKeyEnv = name

	host, err := boyan.New(context.Background(), config)
	requireNoError(t, err, "New() error = %v")
	t.Cleanup(func() { _ = host.Close() })

	info, statErr := os.Stat(filepath.Join(config.StateDir, "search"))
	if statErr != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("private search directory: %v %v", info, statErr)
	}

	info, statErr = os.Stat(filepath.Join(config.StateDir, "search", "search.sqlite"))
	if statErr != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("private search database: %v %v", info, statErr)
	}

	assertTreeOmitsBytes(t, config.StateDir, []byte(config.Embeddings.Endpoint), []byte(secret))
}
