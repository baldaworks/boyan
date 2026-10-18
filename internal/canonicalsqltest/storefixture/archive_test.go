package storefixture_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/archive"
)

func publicArchive(
	t *testing.T,
	f *storefixture.Fixture,
	since uint64,
) (uint64, map[string][]archive.BackupRecord) {
	t.Helper()

	var out bytes.Buffer

	end, err := f.Store.(archive.CanonicalBackupWriter).WriteCanonicalBackup(
		t.Context(),
		&out,
		since,
	)
	if err != nil {
		t.Fatal(err)
	}

	result := map[string][]archive.BackupRecord{}

	data := out.Bytes()
	for len(data) > 0 {
		if len(data) < 4 {
			t.Fatal("short frame")
		}

		n := int(binary.BigEndian.Uint32(data))

		data = data[4:]
		if n > len(data) {
			t.Fatal("short body")
		}

		var record archive.BackupRecord
		if err := json.Unmarshal(data[:n], &record); err != nil {
			t.Fatal(err)
		}

		if record.Kind == "record" {
			result[record.Family] = append(result[record.Family], record)
		}

		data = data[n:]
	}

	return end, result
}

func requireArchiveFamilies(
	t *testing.T,
	rows map[string][]archive.BackupRecord,
	families ...string,
) {
	t.Helper()

	for _, family := range families {
		if len(rows[family]) == 0 {
			t.Errorf("writer missing delta for %s", family)
		}
	}
}

//nolint:cyclop,funlen // Public writer transitions are checked against one incremental chain.
func TestPublicArchiveActualWriterInventory(t *testing.T) {
	forEachMutation(
		t,
		func(t *testing.T, f *storefixture.Fixture, s mutationStore, _ mutationProbe) {
			base, ready := publicArchive(t, f, 0)
			requireArchiveFamilies(t, ready, "source-privacy-ready")
			m := nativeMutation(t)
			certifyMutation(t, &m)

			m.Delivery = []memory.DeliveryOutboxRecord{
				{
					DeliveryID:  "delivery",
					OperationID: m.Operation.OperationID,
					CreatedAt:   m.Operation.CommittedAt,
					PayloadRef:  m.Revisions[0].Payload,
				},
			}
			if _, err := s.ApplyCanonicalMutation(t.Context(), m); err != nil {
				t.Fatal(err)
			}

			claim := memory.DeliveryClaimRequest{
				Scope:      m.Scope,
				LeaseOwner: "worker",
				Now:        m.Operation.CommittedAt.Add(time.Minute),
				LeaseUntil: m.Operation.CommittedAt.Add(2 * time.Minute),
				Limit:      1,
			}
			end, rows := publicArchive(t, f, base)
			requireArchiveFamilies(
				t,
				rows,
				"scope",
				"operation",
				"source",
				"message",
				"item",
				"revision",
				"head",
				"change",
				"payload",
				"source-revision",
				"delivery",
				"source-privacy",
				"turn-certificate",
			)

			if _, err := f.Store.(deliveryStore).ClaimDeliveryOutbox(
				t.Context(),
				claim,
			); err != nil {
				t.Fatal(err)
			}

			record := ingestRecord(1)
			if err := f.Store.(memory.IngestSequenceStore).SaveIngestSequence(
				t.Context(),
				record,
			); err != nil {
				t.Fatal(err)
			}

			meta := f.Store.(metadataStore)

			manifest := testManifest(t, "generation")
			if err := meta.MarkProjectionDirty(t.Context(), manifest); err != nil {
				t.Fatal(err)
			}

			if err := meta.ActivateProjectionGeneration(
				t.Context(),
				manifest,
				manifest.UpdatedAt,
			); err != nil {
				t.Fatal(err)
			}

			if err := meta.SetProjectionRetentionFloor(
				t.Context(),
				manifest.Scope,
				manifest.ProjectionID,
				1,
			); err != nil {
				t.Fatal(err)
			}

			cp := memory.ScopeCheckpoint{
				SchemaVersion: memory.ScopeCheckpointSchemaVersion,
				Scope:         manifest.Scope,
				Kind:          memory.ScopeCheckpointBoundary,
				CheckpointID:  "checkpoint",
				ScopeVersion:  1,
				ChangeSeq:     1,
				OccurredAt:    manifest.UpdatedAt,
				UpdatedAt:     manifest.UpdatedAt,
			}
			if err := meta.SaveScopeCheckpoint(t.Context(), cp); err != nil {
				t.Fatal(err)
			}

			base, rows = publicArchive(t, f, end)
			requireArchiveFamilies(
				t,
				rows,
				"delivery-claim",
				"ingest-sequence",
				"ingest-cursor",
				"projection-manifest",
				"projection-active",
				"projection-retention",
				"projection-checkpoint",
			)

			r := memory.ScopeEraseRequest{
				Scope:                m.Scope,
				OperationID:          "erase",
				Fingerprint:          "erase",
				ExpectedScopeVersion: 1,
				ForgottenAt:          time.Now(),
			}

			forget := f.Store.(forgetStore)
			if _, err := forget.BeginScopeErase(t.Context(), r); err != nil {
				t.Fatal(err)
			}

			end, rows = publicArchive(t, f, base)
			requireArchiveFamilies(t, rows, "erase-journal", "erase-fence", "erase-guard")

			if _, err := forget.RunScopeErase(t.Context(), r); err != nil {
				t.Fatal(err)
			}

			_, rows = publicArchive(t, f, end)
			requireArchiveFamilies(
				t,
				rows,
				"denied-source",
				"denied-revision",
				"forget-outcome",
				"erased-blob",
				"payload",
				"erase-lock-ref",
				"erase-blob-lock",
				"erase-source-identity",
			)

			for _, row := range rows["payload"] {
				if !row.Deleted || len(row.Envelope) != 0 {
					t.Fatal("erased payload retained")
				}
			}

			if err := f.Store.(archive.CanonicalIntegrityVerifier).VerifyCanonicalIntegrity(
				t.Context(),
			); err != nil {
				t.Fatal(err)
			}
		},
	)
}
