package memory

import "time"

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

// PayloadRef separates structural records from content storage. Digest and
// ByteSize bind the reference to the exact payload bytes without coupling the
// portable contract to a storage backend.
type PayloadRef struct {
	ID       string `json:"id"`
	Digest   string `json:"digest"`
	ByteSize uint32 `json:"byte_size"`
}
