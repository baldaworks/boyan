package storefixture_test

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/archive"
)

//nolint:cyclop,funlen // Verify receipt, checkpoint, lease authority and later local backup after reopen.
func TestPublicLogicalReceiptsClaimsCheckpointsAndLocalBackup(t *testing.T) {
	forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, s mutationStore, _ mutationProbe) {
		claimRequest := seedDeliveries(t, s, "delivery")

		claims, err := f.Store.(deliveryStore).ClaimDeliveryOutbox(t.Context(), claimRequest)
		if err != nil || len(claims) != 1 {
			t.Fatalf("claims=%+v %v", claims, err)
		}

		record := ingestRecord(1)
		if err := f.Store.(memory.IngestSequenceStore).SaveIngestSequence(t.Context(), record); err != nil {
			t.Fatal(err)
		}

		checkpoint := memory.ScopeCheckpoint{
			SchemaVersion: memory.ScopeCheckpointSchemaVersion,
			Scope:         record.Scope,
			Kind:          memory.ScopeCheckpointTurn,
			CheckpointID:  "logical",
			ChangeSeq:     42,
			OccurredAt:    time.Now().UTC(),
			UpdatedAt:     time.Now().UTC(),
		}
		if err := f.Store.(memory.ScopeCheckpointStore).SaveScopeCheckpoint(t.Context(), checkpoint); err != nil {
			t.Fatal(err)
		}

		data := publicRestoreFull(t, f.Store, true)
		d := publicRestoreDestination(t, f.Backend)

		_, localBefore := publicRestoreBytes(t, d.Store.(archive.CanonicalBackupWriter), 0)
		if err := restorePublicArtifact(t.Context(), d.Store.(restoredStore), data, true); err != nil {
			t.Fatal(err)
		}

		if err := d.Store.Close(); err != nil {
			t.Fatal(err)
		}

		d.Store, err = d.Open(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		r, found, err := d.Store.(memory.IngestSequenceStore).LoadIngestSequence(
			t.Context(),
			record.ProducerID,
			record.Scope,
			1,
		)
		if err != nil || !found || !reflect.DeepEqual(r, record) {
			t.Fatalf("receipt=%+v %v", r, err)
		}

		got, found, err := d.Store.(memory.ScopeCheckpointStore).LoadScopeCheckpoint(
			t.Context(),
			checkpoint.Scope,
			checkpoint.Kind,
		)
		if err != nil || !found || !reflect.DeepEqual(got, checkpoint) {
			t.Fatalf("checkpoint=%+v %v", got, err)
		}
		// Imported lease ownership remains authoritative after restart.
		settle := memory.DeliverySettlementRequest{
			Scope:       claimRequest.Scope,
			DeliveryID:  "delivery",
			LeaseOwner:  "foreign",
			Status:      memory.DeliveryStatusDelivered,
			CompletedAt: claimRequest.Now,
		}
		if err := d.Store.(deliveryStore).SettleDeliveryOutbox(t.Context(), settle); err == nil {
			t.Fatal("foreign owner settled imported claim")
		}

		settle.LeaseOwner = claimRequest.LeaseOwner
		if err := d.Store.(deliveryStore).SettleDeliveryOutbox(t.Context(), settle); err != nil {
			t.Fatal(err)
		}
		// Actual local mutation and returned local backup cursor include imported
		// effects; source lineage never replaces destination clock/stamps.
		m := nativeMutation(t)
		certifyMutation(t, &m)

		gotOutcome, err := d.Store.(mutationStore).ApplyCanonicalMutation(t.Context(), m)
		if err != nil || gotOutcome.ScopeVersion != 1 {
			t.Fatalf("replay=%+v %v", gotOutcome, err)
		}

		_, after := publicRestoreBytes(t, d.Store.(archive.CanonicalBackupWriter), localBefore)
		if after <= localBefore {
			t.Fatal("imported effects not stamped locally")
		}
	})
}

//nolint:cyclop,funlen // Genuine 900-operation history verifies streaming and replay beyond the old ceiling.
func TestPublicLogicalBeyondLegacyRecordCeiling(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		s := f.Store.(mutationStore)
		if err := s.PrepareSourcePrivacy(t.Context()); err != nil {
			t.Fatal(err)
		}

		scope := memory.Scope{Key: "test:logical-large", Kind: memory.ScopeKindPersonal}

		var last memory.CanonicalMutation

		for i := 0; i < 900; i++ {
			id := fmt.Sprintf("revision-%d", i)
			revision := memory.MemoryRevision{
				SchemaVersion: memory.MemorySchemaVersion,
				RevisionID:    id,
				ItemID:        id,
				Revision:      1,
				Temporal:      memory.Temporal{ObservedAt: time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)},
				Evidence: []memory.EvidenceRef{
					{
						SourceID:      "source",
						MessageID:     "message",
						Role:          memory.MessageRoleUser,
						StartByte:     1,
						EndByte:       2,
						AssertionMode: memory.AssertionModeUser,
					},
				},
				Sensitivity: memory.SensitivityStandard,
				Retention:   memory.RetentionClassStandard,
				Payload:     memory.PayloadRef{ID: "payload", Digest: "digest", ByteSize: 1},
			}

			last = memory.CanonicalMutation{
				SchemaVersion:        memory.CanonicalSchemaVersionV1,
				Scope:                scope,
				ExpectedScopeVersion: uint64(i),
				Operation: memory.OperationRecord{
					OperationID: id,
					Fingerprint: id,
					CommittedAt: revision.Temporal.ObservedAt,
				},
				Items: []memory.MemoryItem{
					{ItemID: id, Scope: scope, Kind: memory.MemoryKindEvent},
				},
				Revisions: []memory.MemoryRevision{revision},
				Heads:     []memory.ItemHead{{ItemID: id, RevisionID: id}},
			}
			if _, err := s.ApplyCanonicalMutation(t.Context(), last); err != nil {
				t.Fatalf("mutation %d: %v", i, err)
			}
		}

		data := publicRestoreFull(t, f.Store, true)
		d := publicRestoreDestination(t, f.Backend)

		summary, err := d.Store.(archive.CanonicalLogicalImporter).ImportCanonicalLogicalWithOptions(
			t.Context(),
			bytes.NewReader(data),
			archive.CanonicalLogicalImportOptions{BatchSize: 256},
		)
		if err != nil || summary.RecordCount <= 4096 {
			t.Fatalf("large import count=%d %v", summary.RecordCount, err)
		}

		state, err := d.Store.(mutationStore).LoadScopeState(t.Context(), scope)
		if err != nil || state.Version != 900 || state.ChangeSeq != 900 {
			t.Fatalf("state=%+v %v", state, err)
		}

		if replay, err := d.Store.(mutationStore).ApplyCanonicalMutation(
			t.Context(),
			last,
		); err != nil ||
			replay.ScopeVersion != 900 {
			t.Fatalf("large replay=%+v %v", replay, err)
		}
	})
}
