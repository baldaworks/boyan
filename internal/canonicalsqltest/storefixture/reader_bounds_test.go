package storefixture_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestPublicReaderResolvesLegacyInheritedBounds(t *testing.T) {
	forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, p mutationProbe) {
		parent := nativeMutation(t)
		expiry := parent.Operation.CommittedAt.Add(time.Hour + 123*time.Nanosecond)
		parent.Revisions[0].Temporal.ExpiresAt = &expiry

		parent.Revisions[0].Retention = memory.RetentionClassEphemeral
		if _, err := store.ApplyCanonicalMutation(t.Context(), parent); err != nil {
			t.Fatal(err)
		}

		child := readerLegacyChild(t, parent)
		if _, err := store.ApplyCanonicalMutation(t.Context(), child); err != nil {
			t.Fatal(err)
		}

		reader := publicReader(t, f.Store)

		records, err := reader.LoadRecallRecords(t.Context(), parent.Scope, []string{"child"})
		if err != nil || len(records) != 1 {
			t.Fatalf("legacy bounds records=%+v err=%v", records, err)
		}

		if records[0].Temporal.ExpiresAt == nil || !records[0].Temporal.ExpiresAt.Equal(expiry) ||
			records[0].Retention != memory.RetentionClassEphemeral {
			t.Fatalf("lost inherited bounds=%+v", records[0])
		}

		p.write(t, canonicalsql.Key{Family: canonicalsql.DeniedRevision, Scope: parent.Scope,
			Parts: [][]byte{[]byte("revision")}}, map[string]any{"revision_id": "revision", "denied_at": time.Now()})

		records, err = reader.LoadRecallRecords(t.Context(), parent.Scope, []string{"child"})
		if err == nil || len(records) != 0 {
			t.Fatalf("denied ancestor returned=%+v err=%v", records, err)
		}
	})
}

func readerLegacyChild(t *testing.T, parent memory.CanonicalMutation) memory.CanonicalMutation {
	t.Helper()

	child := laterMutation(parent, "child", 1)
	child.Items = []memory.MemoryItem{{ItemID: "child-item", Scope: parent.Scope,
		Kind: memory.MemoryKindState, MemoryKey: "derived:profile"}}
	child.Revisions[0].ItemID = "child-item"
	child.Revisions[0].Parents = []string{"revision"}
	child.Revisions[0].Temporal.ExpiresAt = nil
	child.Revisions[0].Retention = memory.RetentionClassStandard
	child.Heads = []memory.ItemHead{{ItemID: "child-item", RevisionID: "child"}}
	compat := memory.CanonicalCompatibilityPayload{SchemaVersion: memory.CanonicalCompatibilitySchemaVersion,
		Kind: memory.DerivedKindProfile, Text: "profile", LegacyItemID: "legacy-profile", LegacyRevisionID: "legacy-child",
		LegacyOperationID: "legacy-operation", LegacyParents: []memory.RevisionRef{{ItemID: "item", RevisionID: "revision"}}}

	data, err := json.Marshal(compat)
	if err != nil {
		t.Fatal(err)
	}

	child.Revisions[0].Payload = privacyRef("profile-payload", data)
	child.Payloads = []memory.CanonicalPayload{{Ref: child.Revisions[0].Payload, Data: data}}

	return child
}
