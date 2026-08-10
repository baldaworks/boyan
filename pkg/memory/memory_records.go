package memory

import "time"

const (
	// MemorySchemaVersion identifies the portable immutable-memory contract.
	MemorySchemaVersion = "boyan-memory/v1"
	maxMemoryTextBytes  = 32 * 1024
)

// MemoryKind distinguishes stable state from a distinct episodic event.
type MemoryKind string

const (
	MemoryKindState MemoryKind = "state"
	MemoryKindEvent MemoryKind = "event"
)

// MemoryKey is an application-derived stable semantic identity for state memory.
type MemoryKey string

// AssertionMode identifies the evidence authority used for a memory revision.
type AssertionMode string

const (
	AssertionModeUser        AssertionMode = "user"
	AssertionModeTrustedTool AssertionMode = "trusted_tool"
)

// Sensitivity controls whether data is eligible for durable retention.
type Sensitivity string

const (
	SensitivityStandard  Sensitivity = "standard"
	SensitivitySensitive Sensitivity = "sensitive"
)

// RetentionClass controls lifecycle handling for stored payloads.
type RetentionClass string

const (
	RetentionClassStandard  RetentionClass = "standard"
	RetentionClassEphemeral RetentionClass = "ephemeral"
)

// Temporal records the interpreted time semantics without discarding source text.
type Temporal struct {
	ObservedAt        time.Time  `json:"observed_at"`
	ValidFrom         *time.Time `json:"valid_from,omitempty"`
	ValidUntil        *time.Time `json:"valid_until,omitempty"`
	EventAt           *time.Time `json:"event_at,omitempty"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	TemporalText      string     `json:"temporal_text,omitempty"`
	TemporalPrecision string     `json:"temporal_precision,omitempty"`
	Timezone          string     `json:"timezone,omitempty"`
}

// EvidenceRef pinpoints a UTF-8 byte span in an immutable message.
type EvidenceRef struct {
	SourceID      string        `json:"source_id"`
	MessageID     string        `json:"message_id"`
	Role          MessageRole   `json:"role"`
	StartByte     uint32        `json:"start_byte"`
	EndByte       uint32        `json:"end_byte"`
	AssertionMode AssertionMode `json:"assertion_mode"`
	TextDigest    string        `json:"text_digest,omitempty"`
}

// CanonicalSourceRecord identifies one durable source independently of its payload.
type CanonicalSourceRecord struct {
	SourceID    string         `json:"source_id"`
	Scope       Scope          `json:"scope"`
	Sensitivity Sensitivity    `json:"sensitivity"`
	Retention   RetentionClass `json:"retention"`
	Payload     PayloadRef     `json:"payload"`
}

// MessageRecord identifies one role-bearing UTF-8 message in a source.
type MessageRecord struct {
	MessageID string      `json:"message_id"`
	SourceID  string      `json:"source_id"`
	Role      MessageRole `json:"role"`
	Payload   PayloadRef  `json:"payload"`
}

// LifecycleEventType records immutable item lifecycle transitions.
type LifecycleEventType string

const (
	LifecycleEventActivate   LifecycleEventType = "activate"
	LifecycleEventSupersede  LifecycleEventType = "supersede"
	LifecycleEventInvalidate LifecycleEventType = "invalidate"
	LifecycleEventForget     LifecycleEventType = "forget"
)

// LifecycleEvent records an application-owned lifecycle change for one revision.
type LifecycleEvent struct {
	EventID    string             `json:"event_id"`
	RevisionID string             `json:"revision_id"`
	Type       LifecycleEventType `json:"type"`
	OccurredAt time.Time          `json:"occurred_at"`
}

// MemoryCandidate is untrusted derivation output. It deliberately has no
// persistent item, revision, scope, or lifecycle identifiers.
type MemoryCandidate struct {
	Kind        MemoryKind     `json:"kind"`
	Statement   string         `json:"statement"`
	Temporal    Temporal       `json:"temporal"`
	Evidence    []EvidenceRef  `json:"evidence"`
	Sensitivity Sensitivity    `json:"sensitivity"`
	Retention   RetentionClass `json:"retention"`
}

// PayloadRef separates structural records from content storage. Digest and
// ByteSize bind the reference to the exact payload bytes without coupling the
// portable contract to a storage backend.
type PayloadRef struct {
	ID       string `json:"id"`
	Digest   string `json:"digest"`
	ByteSize uint32 `json:"byte_size"`
}

// MemoryItem is an application-owned logical identity. Callers never supply its ID.
type MemoryItem struct {
	ItemID    string     `json:"item_id"`
	Scope     Scope      `json:"scope"`
	Kind      MemoryKind `json:"kind"`
	MemoryKey MemoryKey  `json:"memory_key,omitempty"`
}

// MemoryRevision is immutable canonical metadata for one memory change.
type MemoryRevision struct {
	SchemaVersion string         `json:"schema_version"`
	RevisionID    string         `json:"revision_id"`
	ItemID        string         `json:"item_id"`
	Revision      uint64         `json:"revision"`
	Parents       []string       `json:"parents,omitempty"`
	Temporal      Temporal       `json:"temporal"`
	Evidence      []EvidenceRef  `json:"evidence"`
	Sensitivity   Sensitivity    `json:"sensitivity"`
	Retention     RetentionClass `json:"retention"`
	Payload       PayloadRef     `json:"payload"`
}
