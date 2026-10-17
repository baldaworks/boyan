package standaloneruntime //nolint:testpackage // Independent canonical fixture, not a frozen regression overlay.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/baldaworks/boyan/pkg/memory"
)

func denialPayload(id string, data []byte) memory.CanonicalPayload {
	digest := sha256.Sum256(data)

	return memory.CanonicalPayload{
		Ref: memory.PayloadRef{ID: id, Digest: hex.EncodeToString(digest[:]), ByteSize: uint32(len(data))}, Data: data,
	}
}

// Uses the supported archived SourceRecord and legacy ExportID identity. The
// real resolver proves the mapping before erasure; no fallback identity is guessed.
//
//nolint:funlen // One bounded canonical mutation fixture supports native and archived source identities.
func denialSeedPopulation(t *testing.T, r *Runtime, scope memory.Scope, id string, count int, migrated bool) (
	memory.ForgetSourceCommand, []memory.MemoryRevision,
) {
	t.Helper()

	at := time.Now().UTC().Add(-time.Hour)
	turn, err := memory.NewTurn(scope,
		memory.ConversationRef{ConversationID: "denial-conversation", AgentConversationID: "denial-agent"},
		id, at, "Bishkek source "+id, "acknowledged")
	requireNoError(t, err, "population turn: %v")

	ref := memory.SourceRef{Scope: scope, ExportID: turn.ExportID,
		ConversationID: turn.Conversation.ConversationID, SourceTurnID: turn.SourceTurnID}

	sourceID := turn.SourceID
	if migrated {
		sourceID = turn.ExportID
	}

	archive := memory.SourceRecord{
		SchemaVersion: memory.DerivedSchemaVersionV1, Ref: ref, State: memory.SourceStateActive, Turn: &turn,
	}
	requireNoError(t, archive.Validate(), "archived source: %v")
	data, err := json.Marshal(archive)
	requireNoError(t, err, "archive: %v")

	source := denialPayload("denial-source-"+id, data)
	message := denialPayload("denial-message-"+id, []byte(turn.Messages[0].Text))
	evidence, err := memory.NewEvidenceRef(sourceID, turn.Messages[0].MessageID, memory.MessageRoleUser,
		turn.Messages[0].Text, 0, uint32(len(turn.Messages[0].Text)), memory.AssertionModeUser)
	requireNoError(t, err, "evidence: %v")

	var revisions []memory.MemoryRevision

	for offset := 0; offset < count; offset += 100 {
		state, stateErr := r.service.store.LoadScopeState(t.Context(), scope)
		requireNoError(t, stateErr, "population state: %v")

		mutation := memory.CanonicalMutation{
			SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: scope, ExpectedScopeVersion: state.Version,
			Operation: memory.OperationRecord{OperationID: fmt.Sprintf("denial-%s-%d", id, offset),
				Fingerprint: fmt.Sprintf("denial-%s-%d", id, offset), CommittedAt: at},
		}
		if offset == 0 {
			mutation.Sources = []memory.CanonicalSourceRecord{{Scope: scope, SourceID: sourceID, Payload: source.Ref,
				Sensitivity: memory.SensitivityStandard, Retention: memory.RetentionClassStandard}}
			mutation.Messages = []memory.MessageRecord{{MessageID: turn.Messages[0].MessageID,
				SourceID: sourceID, Role: memory.MessageRoleUser, Payload: message.Ref}}
			mutation.Payloads = []memory.CanonicalPayload{source, message}
		}

		for i := offset; i < min(offset+100, count); i++ {
			key := fmt.Sprintf("%s-%05d", id, i)
			payload := denialPayload("denial-text-"+key, []byte("Bishkek memory "+key))
			digest := sha256.Sum256([]byte(key))
			item := memory.MemoryItem{Scope: scope, ItemID: memory.StableID("item", digest[:]),
				Kind: memory.MemoryKindState, MemoryKey: memory.MemoryKey(key)}
			revision := memory.MemoryRevision{SchemaVersion: memory.MemorySchemaVersion,
				RevisionID: memory.StableID("revision", digest[:]), ItemID: item.ItemID, Revision: 1,
				Payload: payload.Ref, Temporal: memory.Temporal{ObservedAt: at}, Evidence: []memory.EvidenceRef{evidence},
				Sensitivity: memory.SensitivityStandard, Retention: memory.RetentionClassStandard}
			mutation.Items = append(mutation.Items, item)
			mutation.Revisions = append(mutation.Revisions, revision)
			mutation.Heads = append(mutation.Heads, memory.ItemHead{ItemID: item.ItemID, RevisionID: revision.RevisionID})
			mutation.Payloads = append(mutation.Payloads, payload)
			revisions = append(revisions, revision)
		}

		_, err = r.service.store.ApplyCanonicalMutation(t.Context(), mutation)
		requireNoError(t, err, "canonical population: %v")
	}

	resolved, err := r.service.store.CanonicalSourceID(t.Context(), scope, ref)
	requireNoError(t, err, "real source resolution: %v")

	if resolved != sourceID {
		t.Fatal("fixture source identity mismatch")
	}

	return memory.ForgetSourceCommand{
		SchemaVersion: memory.DerivedSchemaVersionV1, Source: ref, ForgottenAt: time.Now().UTC(),
	}, revisions
}
