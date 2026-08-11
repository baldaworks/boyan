package memory

import "time"

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
