package memory_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestMemoryRevisionRejectsUnprovenGroundingMetadata(t *testing.T) {
	t.Parallel()

	revision := groundingRevisionFixture()

	raw, err := json.Marshal(revision)
	if err != nil {
		t.Fatal(err)
	}

	var base map[string]any
	if err := json.Unmarshal(raw, &base); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name    string
		version int
		parents []string
		valid   bool
	}{
		{"unsupported", 2, []string{"parent"}, false}, {"unversioned_parents", 0, []string{"parent"}, false},
		{"missing_parents", 1, nil, false}, {"foreign_parent", 1, []string{"unknown"}, false},
		{"valid", 1, []string{"parent"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			base["grounding_bounds_version"] = test.version
			base["grounding_parents"] = test.parents

			data, err := json.Marshal(base)
			if err != nil {
				t.Fatal(err)
			}

			var decoded m.MemoryRevision
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}

			if got := decoded.Validate() == nil; got != test.valid {
				t.Errorf("metadata accepted=%v want %v", got, test.valid)
			}
		})
	}
}

func groundingRevisionFixture() m.MemoryRevision {
	return m.MemoryRevision{SchemaVersion: m.MemorySchemaVersion, RevisionID: "revision", ItemID: "item", Revision: 1,
		Parents: []string{"parent"},
		Temporal: m.Temporal{ObservedAt: time.Date(2026,
			time.October,
			13,
			0,
			0,
			0,
			0,
			time.UTC)},

		Evidence: []m.EvidenceRef{{SourceID: "source",
			MessageID: "message",
			Role:      m.MessageRoleUser,
			StartByte: 0,
			EndByte:   1,

			AssertionMode: m.AssertionModeUser}}, Sensitivity: m.SensitivityStandard, Retention: m.RetentionClassStandard,
		Payload: m.PayloadRef{ID: "payload", Digest: strings.Repeat("a", 64), ByteSize: 1}}
}
