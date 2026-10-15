package boyan

import (
	"errors"
	"net/url"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrInvalidEmbeddingsConfig classifies structurally invalid enabled settings.
// Its text deliberately excludes configured values.
var ErrInvalidEmbeddingsConfig = errors.New("Boyan embeddings configuration is invalid")

// ErrInvalidEmbeddingCredential classifies a missing or unsafe configured
// credential without exposing its name or value.
var ErrInvalidEmbeddingCredential = errors.New("Boyan embedding credential is invalid")

const (
	// EmbeddingFailurePolicyLexical preserves lexical recall when semantic
	// retrieval cannot participate safely.
	EmbeddingFailurePolicyLexical EmbeddingFailurePolicy = "lexical"

	maxEmbeddingEndpointBytes   = 2048
	maxEmbeddingIdentityBytes   = 256
	maxEmbeddingDimensions      = 4096
	maxEmbeddingPrefixBytes     = 256
	maxEmbeddingEnvironmentName = 256
	maxEmbeddingCredentialBytes = 4096
)

// Normalize returns a deterministic, structurally normalized embedding
// configuration without reading credentials or performing I/O.
func (config EmbeddingsConfig) Normalize() (EmbeddingsConfig, error) {
	if !config.Enabled {
		return EmbeddingsConfig{}, nil
	}

	config.Endpoint = strings.TrimSpace(config.Endpoint)
	config.Model = strings.TrimSpace(config.Model)
	config.Revision = strings.TrimSpace(config.Revision)

	config.APIKeyEnv = strings.TrimSpace(config.APIKeyEnv)
	if config.FailurePolicy == "" {
		config.FailurePolicy = EmbeddingFailurePolicyLexical
	}

	if err := validateEmbeddingsConfig(config); err != nil {
		return EmbeddingsConfig{}, err
	}

	return config, nil
}

// Credential resolves only the explicitly named bearer credential. Disabled
// and unnamed configurations are inert. Returned values must remain ephemeral.
func (config EmbeddingsConfig) Credential() (string, error) {
	normalized, err := config.Normalize()
	if err != nil {
		return "", err
	}

	if !normalized.Enabled || normalized.APIKeyEnv == "" {
		return "", nil
	}

	value, ok := os.LookupEnv(normalized.APIKeyEnv)
	if !ok || value == "" || len(value) > maxEmbeddingCredentialBytes || strings.ContainsAny(value, "\r\n") {
		return "", ErrInvalidEmbeddingCredential
	}

	return value, nil
}

func validateEmbeddingsConfig(config EmbeddingsConfig) error {
	valid := validEmbeddingEndpoint(config.Endpoint) &&
		validEmbeddingIdentity(config.Model) &&
		validEmbeddingIdentity(config.Revision) &&
		config.Dimensions >= 1 && config.Dimensions <= maxEmbeddingDimensions &&
		validEmbeddingPrefix(config.QueryPrefix) &&
		validEmbeddingPrefix(config.PassagePrefix) &&
		config.FailurePolicy == EmbeddingFailurePolicyLexical &&
		validEmbeddingEnvironmentName(config.APIKeyEnv)
	if !valid {
		return ErrInvalidEmbeddingsConfig
	}

	return nil
}

func validEmbeddingEndpoint(value string) bool {
	if value == "" || len(value) > maxEmbeddingEndpointBytes || !utf8.ValidString(value) {
		return false
	}

	parsed, err := url.Parse(value)
	if err != nil || parsed == nil || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return false
	}

	return strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https")
}

func validEmbeddingIdentity(value string) bool {
	if value == "" || len(value) > maxEmbeddingIdentityBytes || !utf8.ValidString(value) {
		return false
	}

	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}

	return true
}

func validEmbeddingPrefix(value string) bool {
	return len(value) <= maxEmbeddingPrefixBytes && utf8.ValidString(value) && !strings.ContainsRune(value, '\x00')
}

func validEmbeddingEnvironmentName(value string) bool {
	if value == "" {
		return true
	}

	if len(value) > maxEmbeddingEnvironmentName || !validEmbeddingEnvironmentInitial(value[0]) {
		return false
	}

	for _, character := range []byte(value[1:]) {
		if !validEmbeddingEnvironmentCharacter(character) {
			return false
		}
	}

	return true
}

func validEmbeddingEnvironmentInitial(value byte) bool {
	return value == '_' || value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func validEmbeddingEnvironmentCharacter(value byte) bool {
	return validEmbeddingEnvironmentInitial(value) || value >= '0' && value <= '9'
}
