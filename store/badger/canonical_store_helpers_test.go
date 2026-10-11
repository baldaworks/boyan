package badger_test

import (
	"testing"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/memory"
	canonicalbadger "github.com/baldaworks/boyan/store/badger"
)

const stateTestRevisionOneID = "revision-1"

func canonicalRevisionMutation(
	scope boyan.Scope,
	expectedVersion uint64,
	operationID string,
	revision boyan.MemoryRevision,
) boyan.CanonicalMutation {
	return boyan.CanonicalMutation{
		SchemaVersion:        boyan.CanonicalSchemaVersionV1,
		Scope:                scope,
		ExpectedScopeVersion: expectedVersion,
		Operation: boyan.OperationRecord{
			OperationID: operationID,
			Fingerprint: operationID + "-fingerprint",
			CommittedAt: time.Date(2026, time.August, 5, 12, 0, int(expectedVersion), 0, time.UTC),
		},
		Items:     []boyan.MemoryItem{canonicalItem(scope, revision.ItemID)},
		Revisions: []boyan.MemoryRevision{revision},
		Heads:     []boyan.ItemHead{{ItemID: revision.ItemID, RevisionID: revision.RevisionID}},
	}
}

func canonicalItem(scope boyan.Scope, itemID string) boyan.MemoryItem {
	return boyan.MemoryItem{ItemID: itemID, Scope: scope, Kind: boyan.MemoryKindEvent}
}

func canonicalRevision(revisionID, itemID string) boyan.MemoryRevision {
	return boyan.MemoryRevision{
		SchemaVersion: boyan.MemorySchemaVersion,
		RevisionID:    revisionID,
		ItemID:        itemID,
		Revision:      1,
		Temporal:      boyan.Temporal{ObservedAt: time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)},
		Evidence: []boyan.EvidenceRef{{
			SourceID: "source-1", MessageID: "message-1", Role: boyan.MessageRoleUser,
			StartByte: 1, EndByte: 2, AssertionMode: boyan.AssertionModeUser,
		}},
		Sensitivity: boyan.SensitivityStandard,
		Retention:   boyan.RetentionClassStandard,
		Payload:     boyan.PayloadRef{ID: "payload-1", Digest: "digest-1", ByteSize: 1},
	}
}

func openCanonicalTestStore(t testing.TB, directory string) *canonicalbadger.CanonicalStore {
	t.Helper()

	store, err := canonicalbadger.OpenCanonicalStore(directory)
	if err != nil {
		t.Fatalf("OpenCanonicalStore(%q) error = %v", directory, err)
	}

	return store
}
func closeCanonicalTestStore(t testing.TB, store *canonicalbadger.CanonicalStore) {
	t.Helper()

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}
