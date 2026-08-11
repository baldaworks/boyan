package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"unicode/utf8"
)

// NewEvidenceRef derives a message-level reference after validating that the
// supplied byte range is a non-empty UTF-8 code-point span. TextDigest binds
// the span to the exact canonical message text without embedding that text in
// the evidence record.
func NewEvidenceRef(
	sourceID string,
	messageID string,
	role MessageRole,
	text string,
	startByte uint32,
	endByte uint32,
	assertionMode AssertionMode,
) (EvidenceRef, error) {
	if !isCanonicalID(sourceID) || !isCanonicalID(messageID) {
		return EvidenceRef{}, invalidDerived("evidence source or message id is invalid")
	}
	if !utf8.ValidString(text) ||
		startByte >= endByte ||
		endByte > uint32(len(text)) ||
		!utf8Boundary(text, startByte) ||
		!utf8Boundary(text, endByte) {
		return EvidenceRef{}, invalidDerived("evidence byte span is invalid")
	}
	digest := sha256.Sum256([]byte(text))
	evidence := EvidenceRef{
		SourceID:      sourceID,
		MessageID:     messageID,
		Role:          role,
		StartByte:     startByte,
		EndByte:       endByte,
		AssertionMode: assertionMode,
		TextDigest:    hex.EncodeToString(digest[:]),
	}
	if err := evidence.Validate(); err != nil {
		return EvidenceRef{}, err
	}
	return evidence, nil
}

func utf8Boundary(text string, offset uint32) bool {
	if offset == 0 || int(offset) == len(text) {
		return true
	}
	return int(offset) < len(text) && utf8.RuneStart(text[offset])
}
