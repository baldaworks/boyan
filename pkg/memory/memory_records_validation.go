package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode/utf8"
)

// Validate verifies a portable item does not accept model-owned identity.
func (i MemoryItem) Validate() error {
	if !isCanonicalID(i.ItemID) {
		return invalidDerived("memory item id is required")
	}
	if err := i.Scope.Validate(); err != nil {
		return err
	}
	switch i.Kind {
	case MemoryKindState:
		if !isCanonicalID(string(i.MemoryKey)) {
			return invalidDerived("state memory key is required")
		}
	case MemoryKindEvent:
		if i.MemoryKey != "" {
			return invalidDerived("event memory cannot have a stable memory key")
		}
	default:
		return invalidDerived("unsupported memory kind")
	}
	return nil
}

// Validate verifies temporal ranges and their source representation.
func (t Temporal) Validate() error {
	if t.ObservedAt.IsZero() {
		return invalidDerived("observed time is required")
	}
	if t.ValidFrom != nil && t.ValidUntil != nil && t.ValidUntil.Before(*t.ValidFrom) {
		return invalidDerived("valid interval is inverted")
	}
	if len(t.TemporalText) > maxMemoryTextBytes || strings.TrimSpace(t.TemporalText) != t.TemporalText {
		return invalidDerived("temporal text is invalid")
	}
	return nil
}

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

// NewEvidenceRef derives a message-level reference after validating that the
// supplied byte range is a non-empty UTF-8 code-point span. TextDigest binds
// the span to the exact canonical message text without embedding that text in
// the evidence record.
func NewEvidenceRef(sourceID, messageID string, role MessageRole, text string, startByte, endByte uint32, assertionMode AssertionMode) (EvidenceRef, error) {
	if !isCanonicalID(sourceID) || !isCanonicalID(messageID) {
		return EvidenceRef{}, invalidDerived("evidence source or message id is invalid")
	}
	if !utf8.ValidString(text) || startByte >= endByte || endByte > uint32(len(text)) || !utf8Boundary(text, startByte) || !utf8Boundary(text, endByte) {
		return EvidenceRef{}, invalidDerived("evidence byte span is invalid")
	}
	digest := sha256.Sum256([]byte(text))
	evidence := EvidenceRef{SourceID: sourceID, MessageID: messageID, Role: role, StartByte: startByte, EndByte: endByte, AssertionMode: assertionMode, TextDigest: hex.EncodeToString(digest[:])}
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

func isSHA256Digest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

func (s CanonicalSourceRecord) Validate() error {
	if !isCanonicalID(s.SourceID) {
		return invalidDerived("source id is required")
	}
	if err := s.Scope.Validate(); err != nil {
		return err
	}
	if err := validateSensitivityRetention(s.Sensitivity, s.Retention); err != nil {
		return err
	}
	return s.Payload.Validate()
}

func (m MessageRecord) Validate() error {
	if !isCanonicalID(m.MessageID) || !isCanonicalID(m.SourceID) {
		return invalidDerived("message identity is invalid")
	}
	if m.Role != MessageRoleUser && m.Role != MessageRoleAssistant && m.Role != MessageRoleTool {
		return invalidDerived("message role is invalid")
	}
	return m.Payload.Validate()
}

func (l LifecycleEvent) Validate() error {
	if !isCanonicalID(l.EventID) || !isCanonicalID(l.RevisionID) || l.OccurredAt.IsZero() {
		return invalidDerived("lifecycle event identity is invalid")
	}
	switch l.Type {
	case LifecycleEventActivate, LifecycleEventSupersede, LifecycleEventInvalidate, LifecycleEventForget:
		return nil
	default:
		return invalidDerived("unsupported lifecycle event")
	}
}

func (c MemoryCandidate) Validate() error {
	if c.Kind != MemoryKindState && c.Kind != MemoryKindEvent {
		return invalidDerived("unsupported memory candidate kind")
	}
	if strings.TrimSpace(c.Statement) != c.Statement || c.Statement == "" || len(c.Statement) > maxMemoryTextBytes {
		return invalidDerived("memory candidate statement is invalid")
	}
	if err := c.Temporal.Validate(); err != nil {
		return err
	}
	if len(c.Evidence) == 0 || len(c.Evidence) > MaxSourcesPerRevision {
		return invalidDerived("memory candidate evidence is invalid")
	}
	if err := validateEvidenceRefs(c.Evidence); err != nil {
		return err
	}
	return validateSensitivityRetention(c.Sensitivity, c.Retention)
}

func (p PayloadRef) Validate() error {
	if !isCanonicalID(p.ID) || !isCanonicalID(p.Digest) || p.ByteSize == 0 {
		return invalidDerived("memory payload reference is invalid")
	}
	return nil
}

// Validate verifies immutable revision metadata.
func (r MemoryRevision) Validate() error {
	if r.SchemaVersion != MemorySchemaVersion || !isCanonicalID(r.RevisionID) || !isCanonicalID(r.ItemID) || r.Revision == 0 {
		return invalidDerived("memory revision identity is invalid")
	}
	if err := r.Temporal.Validate(); err != nil {
		return err
	}
	if len(r.Evidence) == 0 || len(r.Evidence) > MaxSourcesPerRevision {
		return invalidDerived("memory revision evidence is invalid")
	}
	if err := validateEvidenceRefs(r.Evidence); err != nil {
		return err
	}
	if err := validateUniqueMemoryRefs(r.Parents, "memory revision parent"); err != nil {
		return err
	}
	for _, parent := range r.Parents {
		if parent == r.RevisionID {
			return invalidDerived("memory revision cannot parent itself")
		}
	}
	if err := validateSensitivityRetention(r.Sensitivity, r.Retention); err != nil {
		return err
	}
	return r.Payload.Validate()
}

func validateUniqueMemoryRefs(ids []string, name string) error {
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if !isCanonicalID(id) {
			return invalidDerived(name + " is invalid")
		}
		if _, exists := seen[id]; exists {
			return invalidDerived(name + " is duplicated")
		}
		seen[id] = struct{}{}
	}
	return nil
}

func validateSensitivityRetention(sensitivity Sensitivity, retention RetentionClass) error {
	if sensitivity != SensitivityStandard && sensitivity != SensitivitySensitive {
		return invalidDerived("unsupported memory sensitivity")
	}
	if retention != RetentionClassStandard && retention != RetentionClassEphemeral {
		return invalidDerived("unsupported memory retention")
	}
	return nil
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
