package boyan_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	boyan "github.com/baldaworks/boyan/pkg/boyan"
)

func validEmbeddingsConfig() boyan.EmbeddingsConfig {
	return boyan.EmbeddingsConfig{
		Enabled:       true,
		Endpoint:      "http://tei:80/v1/embeddings",
		Model:         "intfloat/multilingual-e5-base",
		Revision:      "d128750597153bb5987e10b1c3493a34e5a4502a",
		Dimensions:    768,
		QueryPrefix:   "query: ",
		PassagePrefix: "passage: ",
		FailurePolicy: boyan.EmbeddingFailurePolicyLexical,
	}
}

func TestEmbeddingsConfigNormalizeDisabledIsInert(t *testing.T) {
	t.Parallel()

	config := boyan.EmbeddingsConfig{
		Endpoint:      "not a URL",
		Dimensions:    -1,
		APIKeyEnv:     "ABSENT_BOYAN_EMBEDDING_SECRET",
		FailurePolicy: "unsupported",
	}

	got, err := config.Normalize()
	requireNoError(t, err, "Normalize() error = %v")

	if got != (boyan.EmbeddingsConfig{}) {
		t.Fatalf("Normalize() = %#v, want inert zero config", got)
	}
}

func TestEmbeddingsConfigNormalizeAcceptsExactBounds(t *testing.T) {
	t.Parallel()

	endpoint := "http://tei/" + strings.Repeat("e", 2037)
	config := boyan.EmbeddingsConfig{
		Enabled:       true,
		Endpoint:      "  " + endpoint + "  ",
		Model:         " " + strings.Repeat("m", 256) + " ",
		Revision:      " " + strings.Repeat("r", 256) + " ",
		Dimensions:    4096,
		QueryPrefix:   strings.Repeat("q", 256),
		PassagePrefix: strings.Repeat("я", 128),
		APIKeyEnv:     " BOYAN_" + strings.Repeat("K", 250) + " ",
	}

	got, err := config.Normalize()
	requireNoError(t, err, "Normalize() error = %v")

	if got.Endpoint != endpoint || got.Model != strings.Repeat("m", 256) ||
		got.Revision != strings.Repeat("r", 256) || got.Dimensions != 4096 ||
		got.QueryPrefix != config.QueryPrefix || got.PassagePrefix != config.PassagePrefix ||
		got.APIKeyEnv != "BOYAN_"+strings.Repeat("K", 250) || got.FailurePolicy != "lexical" {
		t.Fatalf("Normalize() = %#v, want trimmed identity and lexical default", got)
	}
}

func TestEmbeddingsConfigNormalizeAcceptsLowerBounds(t *testing.T) {
	t.Parallel()

	config := validEmbeddingsConfig()
	config.Dimensions = 1
	config.QueryPrefix = ""
	config.PassagePrefix = ""

	got, err := config.Normalize()
	requireNoError(t, err, "Normalize() error = %v")

	if got.Dimensions != 1 || got.QueryPrefix != "" || got.PassagePrefix != "" {
		t.Fatalf("Normalize() = %#v, want dimension 1 and empty prefixes", got)
	}
}

func TestEmbeddingsConfigNormalizeRejectsInvalidEnabledShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*boyan.EmbeddingsConfig)
	}{
		{"missing endpoint", func(c *boyan.EmbeddingsConfig) { c.Endpoint = "" }},
		{"endpoint over byte bound", func(c *boyan.EmbeddingsConfig) {
			c.Endpoint = "http://tei/" + strings.Repeat("e", 2038)
		}},
		{"endpoint malformed", func(c *boyan.EmbeddingsConfig) { c.Endpoint = "http://tei/%zz" }},
		{"endpoint scheme", func(c *boyan.EmbeddingsConfig) { c.Endpoint = "file:///private" }},
		{"endpoint host", func(c *boyan.EmbeddingsConfig) { c.Endpoint = "https:///v1/embeddings" }},
		{"endpoint userinfo", func(c *boyan.EmbeddingsConfig) { c.Endpoint = "https://operator-secret@tei/v1/embeddings" }},
		{"endpoint fragment", func(c *boyan.EmbeddingsConfig) { c.Endpoint = "https://tei/v1/embeddings#operator-secret" }},
		{"missing model", func(c *boyan.EmbeddingsConfig) { c.Model = "" }},
		{"model over byte bound", func(c *boyan.EmbeddingsConfig) { c.Model = strings.Repeat("m", 257) }},
		{"model invalid UTF-8", func(c *boyan.EmbeddingsConfig) { c.Model = "model\xff" }},
		{"model control", func(c *boyan.EmbeddingsConfig) { c.Model = "model\noperator-secret" }},
		{"missing revision", func(c *boyan.EmbeddingsConfig) { c.Revision = "" }},
		{"revision over byte bound", func(c *boyan.EmbeddingsConfig) { c.Revision = strings.Repeat("r", 257) }},
		{"revision invalid UTF-8", func(c *boyan.EmbeddingsConfig) { c.Revision = "revision\xff" }},
		{"revision control", func(c *boyan.EmbeddingsConfig) { c.Revision = "revision\toperator-secret" }},
		{"dimension zero", func(c *boyan.EmbeddingsConfig) { c.Dimensions = 0 }},
		{"dimension negative", func(c *boyan.EmbeddingsConfig) { c.Dimensions = -1 }},
		{"dimension over bound", func(c *boyan.EmbeddingsConfig) { c.Dimensions = 4097 }},
		{"query prefix invalid UTF-8", func(c *boyan.EmbeddingsConfig) { c.QueryPrefix = "query\xff" }},
		{"query prefix NUL", func(c *boyan.EmbeddingsConfig) { c.QueryPrefix = "query:\x00" }},
		{"query prefix over byte bound", func(c *boyan.EmbeddingsConfig) { c.QueryPrefix = strings.Repeat("q", 257) }},
		{"passage prefix invalid UTF-8", func(c *boyan.EmbeddingsConfig) { c.PassagePrefix = "passage\xff" }},
		{"passage prefix NUL", func(c *boyan.EmbeddingsConfig) { c.PassagePrefix = "passage:\x00" }},
		{"passage prefix over byte bound", func(c *boyan.EmbeddingsConfig) { c.PassagePrefix = strings.Repeat("я", 129) }},
		{"unsupported strict policy", func(c *boyan.EmbeddingsConfig) { c.FailurePolicy = "strict" }},
		{"unknown policy", func(c *boyan.EmbeddingsConfig) { c.FailurePolicy = "operator-secret" }},
		{"environment starts with digit", func(c *boyan.EmbeddingsConfig) { c.APIKeyEnv = "1SECRET" }},
		{"environment contains hyphen", func(c *boyan.EmbeddingsConfig) { c.APIKeyEnv = "BOYAN-SECRET" }},
		{"environment over byte bound", func(c *boyan.EmbeddingsConfig) { c.APIKeyEnv = "B" + strings.Repeat("K", 256) }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config := validEmbeddingsConfig()
			tc.mutate(&config)

			_, err := config.Normalize()
			if !errors.Is(err, boyan.ErrInvalidEmbeddingsConfig) {
				t.Fatalf("Normalize() error = %v, want invalid-configuration classification", err)
			}

			if strings.Contains(err.Error(), "operator-secret") {
				t.Fatalf("Normalize() error leaked configured value: %v", err)
			}
		})
	}
}

func TestConfigValidateIncludesEmbeddingShapeWithoutResolvingCredential(t *testing.T) {
	t.Parallel()

	config := boyan.DefaultConfig()
	config.Embeddings = validEmbeddingsConfig()
	config.Embeddings.APIKeyEnv = "ABSENT_BOYAN_EMBEDDING_SECRET"
	requireNoError(t, config.Validate(), "Config.Validate() error = %v")

	config.Embeddings.Dimensions = 4097
	if err := config.Validate(); !errors.Is(err, boyan.ErrInvalidEmbeddingsConfig) {
		t.Fatalf("Config.Validate() error = %v, want invalid-configuration classification", err)
	}
}

func TestEmbeddingsConfigCredentialIsInertWhenDisabledOrUnnamed(t *testing.T) {
	t.Parallel()

	enabledWithoutCredential := validEmbeddingsConfig()
	for _, config := range []boyan.EmbeddingsConfig{
		{APIKeyEnv: "ABSENT_BOYAN_EMBEDDING_SECRET"},
		enabledWithoutCredential,
	} {
		credential, err := config.Credential()
		requireNoError(t, err, "Credential() error = %v")

		if credential != "" {
			t.Fatalf("Credential() = %q, want empty", credential)
		}
	}
}

func TestEmbeddingsConfigCredentialValidatesEnvironmentValue(t *testing.T) {
	const name = "BOYAN_TEST_EMBEDDING_CREDENTIAL"

	t.Cleanup(func() { _ = os.Unsetenv(name) })

	config := validEmbeddingsConfig()
	config.APIKeyEnv = name

	tests := []struct {
		name  string
		set   bool
		value string
	}{
		{name: "absent"},
		{name: "empty", set: true},
		{name: "carriage return", set: true, value: "operator-secret\rheader"},
		{name: "line feed", set: true, value: "operator-secret\nheader"},
		{name: "over byte bound", set: true, value: strings.Repeat("s", 4097)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			requireNoError(t, os.Unsetenv(name), "Unsetenv() error = %v")

			if tc.set {
				requireNoError(t, os.Setenv(name, tc.value), "Setenv() error = %v")
			}

			_, err := config.Credential()
			if !errors.Is(err, boyan.ErrInvalidEmbeddingCredential) {
				t.Fatalf("Credential() error = %v, want invalid-credential classification", err)
			}

			if strings.Contains(err.Error(), "operator-secret") || strings.Contains(err.Error(), tc.value) && tc.value != "" {
				t.Fatalf("Credential() error leaked credential value: %v", err)
			}
		})
	}

	want := strings.Repeat("s", 4096)
	requireNoError(t, os.Setenv(name, want), "Setenv() error = %v")

	got, err := config.Credential()
	requireNoError(t, err, "Credential() error = %v")

	if got != want {
		t.Fatalf("Credential() byte length = %d, want 4096", len(got))
	}
}

func TestNewResolvesEnabledCredentialBeforeOpeningResources(t *testing.T) {
	const name = "BOYAN_TEST_MISSING_EMBEDDING_CREDENTIAL"
	requireNoError(t, os.Unsetenv(name), "Unsetenv() error = %v")

	config := testHostConfig(t)
	config.Embeddings = validEmbeddingsConfig()
	config.Embeddings.APIKeyEnv = name

	host, err := boyan.New(context.Background(), config)
	if host != nil || !errors.Is(err, boyan.ErrInvalidEmbeddingCredential) {
		t.Fatalf("New() = host:%v error:%v, want nil invalid-credential error", host, err)
	}

	if _, statErr := os.Stat(config.StateDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("state directory stat error = %v, want not-exist", statErr)
	}
}

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

	info, statErr := os.Stat(filepath.Join(config.StateDir, "embeddings"))
	if statErr != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("private semantic directory: %v %v", info, statErr)
	}

	assertTreeOmitsBytes(t, config.StateDir, []byte(config.Embeddings.Endpoint), []byte(secret))
}

func assertTreeOmitsBytes(t *testing.T, root string, prohibited ...[]byte) {
	t.Helper()

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		for _, value := range prohibited {
			if bytes.Contains(data, value) {
				t.Fatalf("state file %q contains prohibited configuration bytes", path)
			}
		}

		return nil
	})
	requireNoError(t, err, "WalkDir() error = %v")
}
