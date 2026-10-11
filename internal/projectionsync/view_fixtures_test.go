package projectionsync //nolint:testpackage // Exercises shared private test fixtures within the package.

import (
	"time"

	. "github.com/baldaworks/boyan/pkg/memory"
)

func projectionViewItem(scope Scope, itemID string) MemoryItem {
	return MemoryItem{
		ItemID:    itemID,
		Scope:     scope,
		Kind:      MemoryKindState,
		MemoryKey: MemoryKey("key-" + itemID),
	}
}

func projectionViewRevision(revisionID, itemID string, revision uint64) MemoryRevision {
	return MemoryRevision{
		SchemaVersion: MemorySchemaVersion,
		RevisionID:    revisionID,
		ItemID:        itemID,
		Revision:      revision,
		Temporal:      Temporal{ObservedAt: projectionViewTime(int(revision))},
		Evidence: []EvidenceRef{
			{
				SourceID:      "source-1",
				MessageID:     "message-1",
				Role:          MessageRoleUser,
				StartByte:     0,
				EndByte:       1,
				AssertionMode: AssertionModeUser,
			},
		},
		Sensitivity: SensitivityStandard,
		Retention:   RetentionClassStandard,
		Payload:     PayloadRef{ID: "payload-" + revisionID, Digest: "digest-1", ByteSize: 1},
	}
}

func projectionViewTime(offset int) time.Time {
	return time.Date(2026, time.August, 6, 0, 0, offset, 0, time.UTC)
}
