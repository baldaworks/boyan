package semantic

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ModelSpace contains only values that define vector compatibility. Endpoint,
// credentials, timeouts, and concurrency are intentionally not representable.
type ModelSpace struct {
	Model         string
	Revision      string
	Dimensions    int
	QueryPrefix   string
	PassagePrefix string
}

// SpaceFingerprint returns the canonical SHA-256 identity of one model space.
func SpaceFingerprint(space ModelSpace) (string, error) {
	if !validIdentity(space.Model) || !validIdentity(space.Revision) ||
		space.Dimensions < 1 || space.Dimensions > MaxEmbeddingDimensions ||
		!validPrefix(space.QueryPrefix) || !validPrefix(space.PassagePrefix) {
		return "", newError(ReasonInvalidInput)
	}

	encoded, err := json.Marshal(struct {
		Model         string `json:"model"`
		Revision      string `json:"revision"`
		Dimensions    int    `json:"dimensions"`
		QueryPrefix   string `json:"query_prefix"`
		PassagePrefix string `json:"passage_prefix"`
		Preprocessing string `json:"preprocessing"`
		Normalization string `json:"normalization"`
	}{
		Model:         space.Model,
		Revision:      space.Revision,
		Dimensions:    space.Dimensions,
		QueryPrefix:   space.QueryPrefix,
		PassagePrefix: space.PassagePrefix,
		Preprocessing: PreprocessingVersion,
		Normalization: NormalizationVersion,
	})
	if err != nil {
		return "", newError(ReasonInvalidInput)
	}

	hash := sha256.Sum256(encoded)

	return hex.EncodeToString(hash[:]), nil
}

func validIdentity(value string) bool {
	if value == "" || len(value) > MaxModelIdentityBytes || !utf8.ValidString(value) {
		return false
	}

	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}

	return true
}

func validPrefix(value string) bool {
	return len(value) <= MaxPrefixBytes && utf8.ValidString(value) && !strings.ContainsRune(value, '\x00')
}
