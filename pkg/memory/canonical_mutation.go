package memory

import "time"

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

// CanonicalPayload couples payload bytes to the structural reference that
// names them. A canonical store must persist this pair in the same transaction
// as the mutation that first references it.
type CanonicalPayload struct {
	Ref  PayloadRef `json:"ref"`
	Data []byte     `json:"data"`
}

// CanonicalMutationOutcome is returned for both a new commit and an exact
// operation replay.
type CanonicalMutationOutcome struct {
	ScopeVersion uint64   `json:"scope_version"`
	ChangeSeq    uint64   `json:"change_seq"`
	RevisionIDs  []string `json:"revision_ids,omitempty"`
}
