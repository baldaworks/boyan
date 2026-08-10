package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

const maxCanonicalMutationRecords = 512

// ItemHead is the mutable active revision pointer for one logical item.
type ItemHead struct {
	ItemID     string `json:"item_id"`
	RevisionID string `json:"revision_id"`
}

// Validate verifies an application-owned active revision pointer.
func (h ItemHead) Validate() error {
	if !isCanonicalID(h.ItemID) || !isCanonicalID(h.RevisionID) {
		return invalidDerived("canonical item head is invalid")
	}
	return nil
}

// OperationRecord preserves exact replay identity and its committed outcome.
// Fingerprint is application-owned and includes the derivation version.
type OperationRecord struct {
	OperationID string    `json:"operation_id"`
	Fingerprint string    `json:"fingerprint"`
	Outcome     []string  `json:"outcome"`
	CommittedAt time.Time `json:"committed_at"`
}

// CanonicalMutation is the bounded atomic persistence unit. Records are
// append-only except Heads and ScopeState, which are the only mutable indexes.
type CanonicalMutation struct {
	SchemaVersion        string                  `json:"schema_version"`
	Scope                Scope                   `json:"scope"`
	ExpectedScopeVersion uint64                  `json:"expected_scope_version"`
	Operation            OperationRecord         `json:"operation"`
	Sources              []CanonicalSourceRecord `json:"sources,omitempty"`
	Messages             []MessageRecord         `json:"messages,omitempty"`
	Items                []MemoryItem            `json:"items,omitempty"`
	Revisions            []MemoryRevision        `json:"revisions,omitempty"`
	Lifecycle            []LifecycleEvent        `json:"lifecycle,omitempty"`
	Heads                []ItemHead              `json:"heads,omitempty"`
	Delivery             []DeliveryOutboxRecord  `json:"delivery,omitempty"`
	Payloads             []CanonicalPayload      `json:"payloads,omitempty"`
}

// Validate verifies one bounded canonical mutation request.
func (m CanonicalMutation) Validate() error {
	if m.SchemaVersion != CanonicalSchemaVersionV1 {
		return invalidDerived("canonical mutation schema version is invalid")
	}
	if err := m.Scope.Validate(); err != nil {
		return err
	}
	if !isCanonicalID(m.Operation.OperationID) || !isCanonicalID(m.Operation.Fingerprint) || m.Operation.CommittedAt.IsZero() {
		return invalidDerived("canonical mutation operation is invalid")
	}
	if err := validateUniqueCanonicalIDs(m.Operation.Outcome, "canonical operation outcome"); err != nil {
		return err
	}
	count := len(m.Sources) + len(m.Messages) + len(m.Items) + len(m.Revisions) + len(m.Lifecycle) + len(m.Heads) + len(m.Delivery) + len(m.Payloads)
	if count == 0 || count > maxCanonicalMutationRecords {
		return invalidDerived("canonical mutation record count is invalid")
	}
	sourceIDs := make([]string, 0, len(m.Sources))
	for _, source := range m.Sources {
		if err := source.Validate(); err != nil {
			return err
		}
		if source.Scope != m.Scope {
			return invalidDerived("canonical source scope does not match mutation")
		}
		sourceIDs = append(sourceIDs, source.SourceID)
	}
	if err := validateUniqueCanonicalIDs(sourceIDs, "canonical source"); err != nil {
		return err
	}
	messageIDs := make([]string, 0, len(m.Messages))
	for _, message := range m.Messages {
		if err := message.Validate(); err != nil {
			return err
		}
		messageIDs = append(messageIDs, message.MessageID)
	}
	if err := validateUniqueCanonicalIDs(messageIDs, "canonical message"); err != nil {
		return err
	}
	itemIDs := make([]string, 0, len(m.Items))
	for _, item := range m.Items {
		if err := item.Validate(); err != nil {
			return err
		}
		if item.Scope != m.Scope {
			return invalidDerived("canonical item scope does not match mutation")
		}
		itemIDs = append(itemIDs, item.ItemID)
	}
	if err := validateUniqueCanonicalIDs(itemIDs, "canonical item"); err != nil {
		return err
	}
	revisionIDs := make([]string, 0, len(m.Revisions))
	for _, revision := range m.Revisions {
		if err := revision.Validate(); err != nil {
			return err
		}
		revisionIDs = append(revisionIDs, revision.RevisionID)
	}
	if err := validateUniqueCanonicalIDs(revisionIDs, "canonical revision"); err != nil {
		return err
	}
	lifecycleIDs := make([]string, 0, len(m.Lifecycle))
	for _, lifecycle := range m.Lifecycle {
		if err := lifecycle.Validate(); err != nil {
			return err
		}
		lifecycleIDs = append(lifecycleIDs, lifecycle.EventID)
	}
	if err := validateUniqueCanonicalIDs(lifecycleIDs, "canonical lifecycle event"); err != nil {
		return err
	}
	headItemIDs := make([]string, 0, len(m.Heads))
	for _, head := range m.Heads {
		if err := head.Validate(); err != nil {
			return err
		}
		headItemIDs = append(headItemIDs, head.ItemID)
	}
	if err := validateUniqueCanonicalIDs(headItemIDs, "canonical item head"); err != nil {
		return err
	}
	deliveryIDs := make([]string, 0, len(m.Delivery))
	for _, delivery := range m.Delivery {
		if err := delivery.Validate(); err != nil {
			return err
		}
		deliveryIDs = append(deliveryIDs, delivery.DeliveryID)
	}
	if err := validateUniqueCanonicalIDs(deliveryIDs, "canonical delivery outbox"); err != nil {
		return err
	}
	payloadIDs := make([]string, 0, len(m.Payloads))
	for _, payload := range m.Payloads {
		if err := payload.Validate(); err != nil {
			return err
		}
		payloadIDs = append(payloadIDs, payload.Ref.ID)
	}
	return validateUniqueCanonicalIDs(payloadIDs, "canonical payload")
}

// CanonicalPayload couples payload bytes to the structural reference that
// names them. A canonical store must persist this pair in the same transaction
// as the mutation that first references it.
type CanonicalPayload struct {
	Ref  PayloadRef `json:"ref"`
	Data []byte     `json:"data"`
}

// Validate verifies a payload blob can safely be persisted atomically with a
// canonical mutation. Digest checking is an integrity/identity check; payload
// protection is deliberately outside this contract.
func (p CanonicalPayload) Validate() error {
	if err := p.Ref.Validate(); err != nil {
		return err
	}
	if len(p.Data) == 0 || len(p.Data) != int(p.Ref.ByteSize) {
		return invalidDerived("canonical payload bytes are invalid")
	}
	digest := sha256.Sum256(p.Data)
	if hex.EncodeToString(digest[:]) != p.Ref.Digest {
		return invalidDerived("canonical payload digest does not match bytes")
	}
	return nil
}

// CanonicalMutationOutcome is returned for both a new commit and an exact
// operation replay.
type CanonicalMutationOutcome struct {
	ScopeVersion uint64   `json:"scope_version"`
	ChangeSeq    uint64   `json:"change_seq"`
	RevisionIDs  []string `json:"revision_ids,omitempty"`
}

// Validate checks one canonical mutation outcome.
func (o CanonicalMutationOutcome) Validate() error {
	if o.ScopeVersion == 0 || o.ChangeSeq == 0 {
		return invalidDerived("canonical mutation outcome is invalid")
	}
	for _, revisionID := range o.RevisionIDs {
		if !isCanonicalID(revisionID) {
			return invalidDerived("canonical mutation outcome revision is invalid")
		}
	}
	return nil
}
