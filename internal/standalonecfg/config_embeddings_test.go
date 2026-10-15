package standalonecfg_test

import (
	"os"
	"path/filepath"
	"testing"

	standalonecfg "github.com/baldaworks/boyan/internal/standalonecfg"
	boyan "github.com/baldaworks/boyan/pkg/boyan"
)

const embeddingEnvironmentYAML = `embeddings:
  enabled: false
  endpoint: http://yaml:80/v1/embeddings
  model: yaml-model
  revision: yaml-revision
  dimensions: 384
  query_prefix: 'yaml-query: '
  passage_prefix: 'yaml-passage: '
  api_key_env: YAML_EMBEDDING_TOKEN
  failure_policy: lexical
`

func TestLoadConfigAcceptsStrictEmbeddingKeys(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "boyan.yaml")
	data := []byte(`embeddings:
  enabled: true
  endpoint: http://tei:80/v1/embeddings
  model: intfloat/multilingual-e5-base
  revision: d128750597153bb5987e10b1c3493a34e5a4502a
  dimensions: 768
  query_prefix: 'query: '
  passage_prefix: 'passage: '
  api_key_env: BOYAN_TEST_EMBEDDING_TOKEN
  failure_policy: lexical
`)
	requireNoError(t, os.WriteFile(path, data, 0o600), "WriteFile() error = %v")

	cfg, err := standalonecfg.LoadConfig(path)
	requireNoError(t, err, "LoadConfig() error = %v")

	if !cfg.Embeddings.Enabled || cfg.Embeddings.Endpoint != "http://tei:80/v1/embeddings" ||
		cfg.Embeddings.Model != "intfloat/multilingual-e5-base" ||
		cfg.Embeddings.Revision != "d128750597153bb5987e10b1c3493a34e5a4502a" ||
		cfg.Embeddings.Dimensions != 768 || cfg.Embeddings.QueryPrefix != "query: " ||
		cfg.Embeddings.PassagePrefix != "passage: " ||
		cfg.Embeddings.APIKeyEnv != "BOYAN_TEST_EMBEDDING_TOKEN" ||
		cfg.Embeddings.FailurePolicy != "lexical" {
		t.Fatalf("loaded embeddings = %#v, want exact configured values", cfg.Embeddings)
	}
}

func TestLoadConfigShippedEmbeddingExample(t *testing.T) {
	t.Parallel()

	cfg, err := standalonecfg.LoadConfig("../../boyan.example.yaml")
	requireNoError(t, err, "LoadConfig() error = %v")
	requireNoError(t, cfg.Validate(), "Validate() error = %v")

	if cfg.Embeddings.Enabled {
		t.Fatal("shipped example enables embeddings by default")
	}

	cfg.Embeddings.Enabled = true
	requireNoError(t, cfg.Validate(), "enabled example Validate() error = %v")

	if cfg.Embeddings.Endpoint != "http://127.0.0.1:8080/v1/embeddings" ||
		cfg.Embeddings.Model != "intfloat/multilingual-e5-base" ||
		cfg.Embeddings.Revision != "d128750597153bb5987e10b1c3493a34e5a4502a" ||
		cfg.Embeddings.Dimensions != 768 || cfg.Embeddings.QueryPrefix != "query: " ||
		cfg.Embeddings.PassagePrefix != "passage: " || cfg.Embeddings.APIKeyEnv != "" ||
		cfg.Embeddings.FailurePolicy != "lexical" {
		t.Fatalf("shipped example does not match the pinned model space: %#v", cfg.Embeddings)
	}
}

func TestLoadConfigRejectsUnknownEmbeddingKey(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "boyan.yaml")
	data := []byte("embeddings:\n  enabled: false\n  failure_polciy: lexical\n")
	requireNoError(t, os.WriteFile(path, data, 0o600), "WriteFile() error = %v")

	if _, err := standalonecfg.LoadConfig(path); err == nil {
		t.Fatal("LoadConfig() error = nil, want unknown embedding-key rejection")
	}
}

func TestLoadConfigWithoutEmbeddingsPreservesZeroBlock(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "boyan.yaml")
	requireNoError(t, os.WriteFile(path, []byte("log:\n  level: debug\n"), 0o600), "WriteFile() error = %v")

	cfg, err := standalonecfg.LoadConfig(path)
	requireNoError(t, err, "LoadConfig() error = %v")

	if cfg.Embeddings.Enabled || cfg.Embeddings.Endpoint != "" || cfg.Embeddings.APIKeyEnv != "" {
		t.Fatalf("legacy config embeddings = %#v, want inert zero block", cfg.Embeddings)
	}
}

func TestLoadConfigEmbeddingsHonorEnvironmentPrecedence(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{name: "environment only"},
		{name: "partial YAML completed by environment", yaml: "embeddings:\n  enabled: false\n"},
		{name: "environment overrides YAML", yaml: embeddingEnvironmentYAML},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setEmbeddingEnvironment(t)

			path := ""
			if tc.yaml != "" {
				path = filepath.Join(t.TempDir(), "boyan.yaml")
				requireNoError(t, os.WriteFile(path, []byte(tc.yaml), 0o600), "WriteFile() error = %v")
			}

			cfg, err := standalonecfg.LoadConfig(path)
			requireNoError(t, err, "LoadConfig() error = %v")

			if cfg.Embeddings != expectedEnvironmentEmbeddings() {
				t.Fatalf("loaded embeddings = %#v, want environment values", cfg.Embeddings)
			}
		})
	}
}

func TestLoadConfigEmbeddingEmptyEnvironmentPreservesYAMLCredentialReference(t *testing.T) {
	tests := []struct {
		name      string
		apiKeyEnv string
	}{
		{name: "remote credential reference retained", apiKeyEnv: "OLD_TOKEN"},
		{name: "local YAML explicitly clears credential reference", apiKeyEnv: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BOYAN_EMBEDDINGS_API_KEY_ENV", "")
			path := filepath.Join(t.TempDir(), "boyan.yaml")
			data := []byte("embeddings:\n  api_key_env: '" + tc.apiKeyEnv + "'\n")
			requireNoError(t, os.WriteFile(path, data, 0o600), "WriteFile() error = %v")

			cfg, err := standalonecfg.LoadConfig(path)
			requireNoError(t, err, "LoadConfig() error = %v")

			if cfg.Embeddings.APIKeyEnv != tc.apiKeyEnv {
				t.Fatalf("loaded credential reference = %q, want YAML value %q", cfg.Embeddings.APIKeyEnv, tc.apiKeyEnv)
			}
		})
	}
}

func expectedEnvironmentEmbeddings() boyan.EmbeddingsConfig {
	return boyan.EmbeddingsConfig{
		Enabled:       true,
		Endpoint:      "http://env:80/v1/embeddings",
		Model:         "env-model",
		Revision:      "env-revision",
		Dimensions:    768,
		QueryPrefix:   "env-query: ",
		PassagePrefix: "env-passage: ",
		APIKeyEnv:     "ENV_EMBEDDING_TOKEN",
		FailurePolicy: boyan.EmbeddingFailurePolicyLexical,
	}
}

func setEmbeddingEnvironment(t *testing.T) {
	t.Helper()

	values := map[string]string{
		"BOYAN_EMBEDDINGS_ENABLED":        "true",
		"BOYAN_EMBEDDINGS_ENDPOINT":       "http://env:80/v1/embeddings",
		"BOYAN_EMBEDDINGS_MODEL":          "env-model",
		"BOYAN_EMBEDDINGS_REVISION":       "env-revision",
		"BOYAN_EMBEDDINGS_DIMENSIONS":     "768",
		"BOYAN_EMBEDDINGS_QUERY_PREFIX":   "env-query: ",
		"BOYAN_EMBEDDINGS_PASSAGE_PREFIX": "env-passage: ",
		"BOYAN_EMBEDDINGS_API_KEY_ENV":    "ENV_EMBEDDING_TOKEN",
		"BOYAN_EMBEDDINGS_FAILURE_POLICY": "lexical",
	}

	for name, value := range values {
		t.Setenv(name, value)
	}
}
