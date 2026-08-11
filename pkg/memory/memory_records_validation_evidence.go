package memory

import (
	"encoding/hex"
	"strings"
)

// Validate verifies one evidence span and its source authority.
func (e EvidenceRef) Validate() error {
	if !isCanonicalID(e.SourceID) || !isCanonicalID(e.MessageID) || e.EndByte <= e.StartByte {
		return invalidDerived("evidence reference is invalid")
	}
	if e.AssertionMode == AssertionModeUser && e.Role != MessageRoleUser {
		return invalidDerived("user evidence must have user role")
	}
	if e.AssertionMode == AssertionModeTrustedTool && e.Role != MessageRoleTool {
		return invalidDerived("trusted tool evidence role is invalid")
	}
	if e.AssertionMode != AssertionModeUser && e.AssertionMode != AssertionModeTrustedTool {
		return invalidDerived("unsupported evidence assertion mode")
	}
	if e.TextDigest != "" && !isSHA256Digest(e.TextDigest) {
		return invalidDerived("evidence text digest is invalid")
	}
	return nil
}

func isSHA256Digest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

func validateEvidenceRefs(evidence []EvidenceRef) error {
	seen := make(map[EvidenceRef]struct{}, len(evidence))
	for _, ref := range evidence {
		if err := ref.Validate(); err != nil {
			return err
		}
		if _, exists := seen[ref]; exists {
			return invalidDerived("memory evidence contains a duplicate reference")
		}
		seen[ref] = struct{}{}
	}
	return nil
}
