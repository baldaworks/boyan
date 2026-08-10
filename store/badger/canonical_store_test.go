package badger

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/baldaworks/boyan"
	"github.com/baldaworks/boyan/boyantest"
)

const stateTestRevisionOneID = "revision-1"

func TestOpenPortableAPIReopensCanonicalDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "portable.badger")
	store, err := Open(directory, Config{SyncWrites: true, DetectConflicts: true, NumVersionsToKeep: 1})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	reopened, err := Open(directory)
	if err != nil {
		t.Fatalf("Open(reopen) error = %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
}

func TestCanonicalStoreCanonicalContract(t *testing.T) {
	boyantest.RunCanonicalStoreContract(t, func(t *testing.T) boyan.CanonicalStore {
		store, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "memory.badger"))
		if err != nil {
			t.Fatalf("OpenCanonicalStore() error = %v", err)
		}
		t.Cleanup(func() { _ = store.Close() })
		return store
	})
}

func TestCanonicalStoreOwnsOneWritableDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "memory.badger")
	first, err := OpenCanonicalStore(directory)
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	if _, err := OpenCanonicalStore(directory); err == nil {
		t.Fatal("second writable Badger store opened")
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	reopened, err := OpenCanonicalStore(directory)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
}

func TestCanonicalRecordCodec(t *testing.T) {
	store, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "memory.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	key := []byte("record")
	if err := store.db.Update(func(txn *badger.Txn) error {
		return putCanonicalRecord(txn, key, "scope", map[string]string{"key": "value"})
	}); err != nil {
		t.Fatalf("write record error = %v", err)
	}
	var got map[string]string
	if err := store.db.View(func(txn *badger.Txn) error {
		return getCanonicalRecord(txn, key, "scope", &got)
	}); err != nil {
		t.Fatalf("read record error = %v", err)
	}
	if got["key"] != "value" {
		t.Fatalf("record = %#v", got)
	}
	if err := store.db.View(func(txn *badger.Txn) error {
		return getCanonicalRecord(txn, key, "operation", &got)
	}); err == nil {
		t.Fatal("wrong record type decoded")
	}
}

func TestCanonicalStoreRequiresDirectory(t *testing.T) {
	if _, err := OpenCanonicalStore(" "); err == nil {
		t.Fatal("empty directory opened")
	}
}

func TestCanonicalStoreValueLogGCValidation(t *testing.T) {
	store, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "memory.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.RunValueLogGC(0); err == nil {
		t.Fatal("RunValueLogGC(0) succeeded")
	}
	if err := store.RunValueLogGC(0.5); err != nil {
		t.Fatalf("RunValueLogGC() error = %v", err)
	}
}

func TestCanonicalStoreDeniesSourceByExactScope(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "memory.badger")
	store, err := OpenCanonicalStore(directory)
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	scope := boyan.Scope{Key: "canonical:deny-a", Kind: boyan.ScopeKindPersonal}
	if err := store.DenySource(context.Background(), scope, "source-1", time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("DenySource() error = %v", err)
	}
	if err := store.DenySource(context.Background(), scope, "source-1", time.Date(2026, time.August, 5, 12, 1, 0, 0, time.UTC)); err != nil {
		t.Fatalf("DenySource() retry error = %v", err)
	}
	denied, err := store.IsSourceDenied(context.Background(), scope, "source-1")
	if err != nil || !denied {
		t.Fatalf("IsSourceDenied() = %t, error = %v", denied, err)
	}
	otherScope := boyan.Scope{Key: "canonical:deny-b", Kind: boyan.ScopeKindPersonal}
	if denied, err := store.IsSourceDenied(context.Background(), otherScope, "source-1"); err != nil || denied {
		t.Fatalf("other scope IsSourceDenied() = %t, error = %v", denied, err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	store, err = OpenCanonicalStore(directory)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if denied, err := store.IsSourceDenied(context.Background(), scope, "source-1"); err != nil || !denied {
		t.Fatalf("reopened IsSourceDenied() = %t, error = %v", denied, err)
	}
}

func TestCanonicalStoreAppliesCanonicalMutation(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "memory.badger")
	store, err := OpenCanonicalStore(directory)
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	scope := boyan.Scope{Key: "canonical:badger", Kind: boyan.ScopeKindPersonal}
	mutation := boyan.CanonicalMutation{
		SchemaVersion: boyan.CanonicalSchemaVersionV1,
		Scope:         scope,
		Operation: boyan.OperationRecord{
			OperationID: "operation-1",
			Fingerprint: "derivation-v2-sha256",
			CommittedAt: time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC),
		},
		Heads: []boyan.ItemHead{{ItemID: "item-1", RevisionID: "revision-1"}},
	}
	mutation.Items = []boyan.MemoryItem{canonicalItem(scope, "item-1")}
	mutation.Revisions = []boyan.MemoryRevision{canonicalRevision("revision-1", "item-1")}
	payloadData := []byte("canonical payload")
	payloadDigest := sha256.Sum256(payloadData)
	payloadRef := boyan.PayloadRef{ID: "payload-1", Digest: hex.EncodeToString(payloadDigest[:]), ByteSize: uint32(len(payloadData))}
	mutation.Revisions[0].Payload = payloadRef
	mutation.Payloads = []boyan.CanonicalPayload{{Ref: payloadRef, Data: payloadData}}
	first, err := store.ApplyCanonicalMutation(context.Background(), mutation)
	if err != nil {
		t.Fatalf("ApplyCanonicalMutation() error = %v", err)
	}
	if first.ScopeVersion != 1 || first.ChangeSeq != 1 {
		t.Fatalf("outcome = %#v", first)
	}
	gotPayload, err := store.LoadPayload(context.Background(), mutation.Payloads[0].Ref)
	if err != nil || string(gotPayload) != string(payloadData) {
		t.Fatalf("LoadPayload() = %q, error = %v", gotPayload, err)
	}
	replayed, err := store.ApplyCanonicalMutation(context.Background(), mutation)
	if err != nil || replayed.ScopeVersion != first.ScopeVersion || replayed.ChangeSeq != first.ChangeSeq {
		t.Fatalf("replay = %#v, error = %v, want %#v", replayed, err, first)
	}
	state, err := store.LoadScopeState(context.Background(), scope)
	if err != nil || state.Version != 1 || state.ChangeSeq != 1 {
		t.Fatalf("LoadScopeState() = %#v, error = %v", state, err)
	}
	changes, err := store.ScanScopeChanges(context.Background(), scope, 0, 10)
	if err != nil || len(changes) != 1 || changes[0].OperationID != mutation.Operation.OperationID {
		t.Fatalf("ScanScopeChanges() = %#v, error = %v", changes, err)
	}
	changes, err = store.ScanScopeChanges(context.Background(), scope, 1, 10)
	if err != nil || len(changes) != 0 {
		t.Fatalf("ScanScopeChanges(after latest) = %#v, error = %v", changes, err)
	}
	collision := mutation
	collision.Operation.Fingerprint = "different-fingerprint"
	if _, err := store.ApplyCanonicalMutation(context.Background(), collision); err == nil {
		t.Fatal("operation fingerprint collision succeeded")
	}
}

func TestCanonicalStorePersistsBidirectionalProvenance(t *testing.T) {
	store, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "memory.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	scope := boyan.Scope{Key: "canonical:provenance", Kind: boyan.ScopeKindPersonal}
	first := canonicalRevision("revision-1", "item-1")
	if _, err := store.ApplyCanonicalMutation(context.Background(), canonicalRevisionMutation(scope, 0, "operation-1", first)); err != nil {
		t.Fatalf("first ApplyCanonicalMutation() error = %v", err)
	}
	second := canonicalRevision("revision-2", "item-2")
	second.Parents = []string{first.RevisionID}
	if _, err := store.ApplyCanonicalMutation(context.Background(), canonicalRevisionMutation(scope, 1, "operation-2", second)); err != nil {
		t.Fatalf("second ApplyCanonicalMutation() error = %v", err)
	}
	forwardKey, err := badgerProvenanceKey(scope, badgerRecordProvenanceForward, second.RevisionID, first.RevisionID)
	if err != nil {
		t.Fatalf("badgerProvenanceKey() error = %v", err)
	}
	reverseKey, err := badgerProvenanceKey(scope, badgerRecordProvenanceReverse, first.RevisionID, second.RevisionID)
	if err != nil {
		t.Fatalf("badgerProvenanceKey() error = %v", err)
	}
	if err := store.db.View(func(txn *badger.Txn) error {
		var forward, reverse badgerProvenanceEdge
		if err := getCanonicalRecord(txn, forwardKey, badgerRecordProvenanceForward, &forward); err != nil {
			return err
		}
		if err := getCanonicalRecord(txn, reverseKey, badgerRecordProvenanceReverse, &reverse); err != nil {
			return err
		}
		if forward != reverse || forward.ChildRevisionID != second.RevisionID || forward.ParentRevisionID != first.RevisionID {
			t.Fatalf("provenance edges = %#v / %#v", forward, reverse)
		}
		return nil
	}); err != nil {
		t.Fatalf("read provenance edges: %v", err)
	}
	sourceKey, err := badgerProvenanceKey(scope, badgerRecordSourceRevision, "source-1", first.RevisionID)
	if err != nil {
		t.Fatalf("source provenance key error = %v", err)
	}
	if err := store.db.View(func(txn *badger.Txn) error {
		var edge badgerProvenanceEdge
		return getCanonicalRecord(txn, sourceKey, badgerRecordSourceRevision, &edge)
	}); err != nil {
		t.Fatalf("read source provenance edge: %v", err)
	}
}

func TestCanonicalStoreBatchesSourceProvenance(t *testing.T) {
	store, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "memory.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	scope := boyan.Scope{Key: "canonical:source-batch", Kind: boyan.ScopeKindPersonal}
	for index, revisionID := range []string{"revision-1", "revision-2"} {
		mutation := canonicalRevisionMutation(scope, uint64(index), "operation-"+revisionID, canonicalRevision(revisionID, "item-"+revisionID))
		if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
			t.Fatalf("ApplyCanonicalMutation(%s) error = %v", revisionID, err)
		}
	}
	first, cursor, err := store.SourceRevisionBatch(context.Background(), scope, "source-1", "", 1)
	if err != nil || len(first) != 1 || cursor != first[0] {
		t.Fatalf("first SourceRevisionBatch() = %#v, %q, error = %v", first, cursor, err)
	}
	second, cursor, err := store.SourceRevisionBatch(context.Background(), scope, "source-1", cursor, 1)
	if err != nil || len(second) != 1 || second[0] == first[0] || cursor != second[0] {
		t.Fatalf("second SourceRevisionBatch() = %#v, %q, error = %v", second, cursor, err)
	}
	last, cursor, err := store.SourceRevisionBatch(context.Background(), scope, "source-1", cursor, 1)
	if err != nil || len(last) != 0 || cursor != "" {
		t.Fatalf("final SourceRevisionBatch() = %#v, %q, error = %v", last, cursor, err)
	}
}

func TestCanonicalStoreRejectsInvalidProvenanceAtomically(t *testing.T) {
	store, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "memory.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	scope := boyan.Scope{Key: "canonical:provenance-invalid", Kind: boyan.ScopeKindPersonal}
	first := canonicalRevision("revision-1", "item-1")
	second := canonicalRevision("revision-2", "item-2")
	first.Parents = []string{second.RevisionID}
	second.Parents = []string{first.RevisionID}
	mutation := canonicalRevisionMutation(scope, 0, "operation-cycle", first)
	mutation.Revisions = append(mutation.Revisions, second)
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err == nil {
		t.Fatal("cyclic provenance mutation succeeded")
	}
	state, err := store.LoadScopeState(context.Background(), scope)
	if err != nil || state.Version != 0 || state.ChangeSeq != 0 {
		t.Fatalf("state after rejected mutation = %#v, error = %v", state, err)
	}
}

func TestCanonicalStoreRejectsDanglingHeadAtomically(t *testing.T) {
	store, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "memory.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	scope := boyan.Scope{Key: "canonical:dangling-head", Kind: boyan.ScopeKindPersonal}
	mutation := boyan.CanonicalMutation{
		SchemaVersion: boyan.CanonicalSchemaVersionV1,
		Scope:         scope,
		Operation:     boyan.OperationRecord{OperationID: "operation-1", Fingerprint: "fingerprint-1", CommittedAt: time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)},
		Heads:         []boyan.ItemHead{{ItemID: "item-1", RevisionID: "missing-revision"}},
	}
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err == nil {
		t.Fatal("dangling head mutation succeeded")
	}
	state, err := store.LoadScopeState(context.Background(), scope)
	if err != nil || state.Version != 0 {
		t.Fatalf("state after rejected dangling head = %#v, error = %v", state, err)
	}
}

func TestCanonicalStoreClaimsAndRecoversDeliveryLease(t *testing.T) {
	store, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "memory.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	scope := boyan.Scope{Key: "canonical:delivery", Kind: boyan.ScopeKindPersonal}
	mutation := boyan.CanonicalMutation{
		SchemaVersion: boyan.CanonicalSchemaVersionV1,
		Scope:         scope,
		Operation:     boyan.OperationRecord{OperationID: "operation-1", Fingerprint: "fingerprint-1", CommittedAt: time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)},
		Delivery: []boyan.DeliveryOutboxRecord{{
			DeliveryID: "delivery-1", OperationID: "operation-1", CreatedAt: time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC),
			PayloadRef: boyan.PayloadRef{ID: "payload-1", Digest: "digest-1", ByteSize: 1},
		}},
	}
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatalf("ApplyCanonicalMutation() error = %v", err)
	}
	now := time.Date(2026, time.August, 5, 12, 1, 0, 0, time.UTC)
	request := boyan.DeliveryClaimRequest{Scope: scope, LeaseOwner: "worker-1", Now: now, LeaseUntil: now.Add(time.Minute), Limit: 1}
	first, err := store.ClaimDeliveryOutbox(context.Background(), request)
	if err != nil || len(first) != 1 || first[0].Claim.Attempts != 1 {
		t.Fatalf("first ClaimDeliveryOutbox() = %#v, error = %v", first, err)
	}
	if claimed, err := store.ClaimDeliveryOutbox(context.Background(), request); err != nil || len(claimed) != 0 {
		t.Fatalf("active lease claim = %#v, error = %v", claimed, err)
	}
	if err := store.SettleDeliveryOutbox(context.Background(), boyan.DeliverySettlementRequest{
		Scope: scope, DeliveryID: "delivery-1", LeaseOwner: "worker-2", Status: boyan.DeliveryStatusDelivered, CompletedAt: now,
	}); err == nil {
		t.Fatal("foreign worker settled an active lease")
	}
	if err := store.SettleDeliveryOutbox(context.Background(), boyan.DeliverySettlementRequest{
		Scope: scope, DeliveryID: "delivery-1", LeaseOwner: "worker-1", Status: boyan.DeliveryStatusDelivered, CompletedAt: now,
	}); err != nil {
		t.Fatalf("SettleDeliveryOutbox() error = %v", err)
	}
	if claimed, err := store.ClaimDeliveryOutbox(context.Background(), request); err != nil || len(claimed) != 0 {
		t.Fatalf("settled delivery claim = %#v, error = %v", claimed, err)
	}
	// Use a distinct delivery to cover lease recovery without disturbing the
	// delivered record above.
	mutation.Operation.OperationID = "operation-2"
	mutation.Operation.Fingerprint = "fingerprint-2"
	mutation.Operation.CommittedAt = now
	mutation.ExpectedScopeVersion = 1
	mutation.Delivery[0].DeliveryID = "delivery-2"
	mutation.Delivery[0].OperationID = "operation-2"
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatalf("second ApplyCanonicalMutation() error = %v", err)
	}
	if claimed, err := store.ClaimDeliveryOutbox(context.Background(), request); err != nil || len(claimed) != 1 || claimed[0].Record.DeliveryID != "delivery-2" {
		t.Fatalf("second delivery claim = %#v, error = %v", claimed, err)
	}
	request.Now = request.LeaseUntil.Add(time.Second)
	request.LeaseUntil = request.Now.Add(time.Minute)
	recovered, err := store.ClaimDeliveryOutbox(context.Background(), request)
	if err != nil || len(recovered) != 1 || recovered[0].Claim.Attempts != 2 {
		t.Fatalf("recovered ClaimDeliveryOutbox() = %#v, error = %v", recovered, err)
	}
}

func TestCanonicalStoreRestoresReplayAndScopeState(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "memory.badger")
	store, err := OpenCanonicalStore(directory)
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	scope := boyan.Scope{Key: "canonical:restart", Kind: boyan.ScopeKindPersonal}
	mutation := canonicalRevisionMutation(scope, 0, "operation-1", canonicalRevision("revision-1", "item-1"))
	first, err := store.ApplyCanonicalMutation(context.Background(), mutation)
	if err != nil {
		t.Fatalf("ApplyCanonicalMutation() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	store, err = OpenCanonicalStore(directory)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	replayed, err := store.ApplyCanonicalMutation(context.Background(), mutation)
	if err != nil || replayed.ScopeVersion != first.ScopeVersion || replayed.ChangeSeq != first.ChangeSeq {
		t.Fatalf("replay after reopen = %#v, error = %v", replayed, err)
	}
	state, err := store.LoadScopeState(context.Background(), scope)
	if err != nil || state.Version != first.ScopeVersion || state.ChangeSeq != first.ChangeSeq {
		t.Fatalf("state after reopen = %#v, error = %v", state, err)
	}
}

func TestCanonicalStoreBackupRestoreAndIntegrityVerification(t *testing.T) {
	ctx := context.Background()
	sourceDir := filepath.Join(t.TempDir(), "source.badger")
	source, err := OpenCanonicalStore(sourceDir)
	if err != nil {
		t.Fatalf("OpenCanonicalStore(source) error = %v", err)
	}
	scope := boyan.Scope{Key: "canonical:backup", Kind: boyan.ScopeKindPersonal}
	mutation := canonicalRevisionMutation(scope, 0, "operation-backup", canonicalRevision("revision-backup", "item-backup"))
	if _, err := source.ApplyCanonicalMutation(ctx, mutation); err != nil {
		t.Fatalf("ApplyCanonicalMutation() error = %v", err)
	}
	var backup bytes.Buffer
	version, err := source.WriteCanonicalBackup(ctx, &backup, 0)
	if err != nil || version == 0 || backup.Len() == 0 {
		t.Fatalf("WriteCanonicalBackup() version=%d bytes=%d error=%v", version, backup.Len(), err)
	}
	if err := source.Close(); err != nil {
		t.Fatalf("source Close() error = %v", err)
	}

	restored, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "restored.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore(restored) error = %v", err)
	}
	t.Cleanup(func() { _ = restored.Close() })
	if err := restored.RestoreCanonicalBackup(ctx, bytes.NewReader(backup.Bytes()), 0); err != nil {
		t.Fatalf("RestoreCanonicalBackup() error = %v", err)
	}
	if err := restored.VerifyCanonicalIntegrity(ctx); err != nil {
		t.Fatalf("VerifyCanonicalIntegrity() error = %v", err)
	}
	replayed, err := restored.ApplyCanonicalMutation(ctx, mutation)
	if err != nil || replayed.ScopeVersion != 1 || len(replayed.RevisionIDs) != 1 {
		t.Fatalf("replayed restored mutation = %+v, error = %v", replayed, err)
	}
}

func TestCanonicalStoreLogicalExportImportRoundTrip(t *testing.T) {
	ctx := context.Background()
	source, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "source.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore(source) error = %v", err)
	}
	scope := boyan.Scope{Key: "canonical:logical-export", Kind: boyan.ScopeKindPersonal}
	mutation := canonicalRevisionMutation(scope, 0, "operation-logical", canonicalRevision("revision-logical", "item-logical"))
	if _, err := source.ApplyCanonicalMutation(ctx, mutation); err != nil {
		t.Fatalf("ApplyCanonicalMutation() error = %v", err)
	}
	var encoded bytes.Buffer
	if err := source.ExportCanonicalLogical(ctx, &encoded); err != nil {
		t.Fatalf("ExportCanonicalLogical() error = %v", err)
	}
	if encoded.Len() == 0 || !bytes.Contains(encoded.Bytes(), []byte("session-memory-logical-export/v1")) {
		t.Fatalf("logical export = %q", encoded.String())
	}
	if err := source.Close(); err != nil {
		t.Fatalf("source Close() error = %v", err)
	}

	destination, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "destination.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore(destination) error = %v", err)
	}
	t.Cleanup(func() { _ = destination.Close() })
	if err := destination.ImportCanonicalLogical(ctx, bytes.NewReader(encoded.Bytes())); err != nil {
		t.Fatalf("ImportCanonicalLogical() error = %v", err)
	}
	if err := destination.VerifyCanonicalIntegrity(ctx); err != nil {
		t.Fatalf("VerifyCanonicalIntegrity() error = %v", err)
	}
	replayed, err := destination.ApplyCanonicalMutation(ctx, mutation)
	if err != nil || replayed.ScopeVersion != 1 || len(replayed.RevisionIDs) != 1 {
		t.Fatalf("logical replay = %+v, error = %v", replayed, err)
	}
}

func TestCanonicalStoreLogicalImportStreamsBeyondLegacyRecordCeiling(t *testing.T) {
	ctx := context.Background()
	source, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "source.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore(source) error = %v", err)
	}
	scope := boyan.Scope{Key: "canonical:logical-large", Kind: boyan.ScopeKindPersonal}
	for index := 0; index < 900; index++ {
		revisionID := fmt.Sprintf("revision-large-%d", index)
		itemID := fmt.Sprintf("item-large-%d", index)
		if _, err := source.ApplyCanonicalMutation(ctx, canonicalRevisionMutation(scope, uint64(index), "operation-large-"+revisionID, canonicalRevision(revisionID, itemID))); err != nil {
			t.Fatalf("ApplyCanonicalMutation(%d) error = %v", index, err)
		}
	}
	var encoded bytes.Buffer
	if err := source.ExportCanonicalLogical(ctx, &encoded); err != nil {
		t.Fatalf("ExportCanonicalLogical() error = %v", err)
	}
	if err := source.Close(); err != nil {
		t.Fatalf("source Close() error = %v", err)
	}

	destination, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "destination.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore(destination) error = %v", err)
	}
	t.Cleanup(func() { _ = destination.Close() })
	if err := destination.ImportCanonicalLogical(ctx, bytes.NewReader(encoded.Bytes())); err != nil {
		t.Fatalf("ImportCanonicalLogical() error = %v", err)
	}
	if err := destination.VerifyCanonicalIntegrity(ctx); err != nil {
		t.Fatalf("VerifyCanonicalIntegrity() error = %v", err)
	}
	state, err := destination.LoadScopeState(ctx, scope)
	if err != nil {
		t.Fatalf("LoadScopeState() error = %v", err)
	}
	if state.Version != 900 || state.ChangeSeq != 900 {
		t.Fatalf("LoadScopeState() = %#v, want version/change_seq 900", state)
	}
}

func TestCanonicalStoreProvidesBoundedRevisionAndHeadReads(t *testing.T) {
	store, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "memory.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	scope := boyan.Scope{Key: "canonical:projection-read", Kind: boyan.ScopeKindPersonal}
	for index := 1; index <= 2; index++ {
		id := fmt.Sprintf("%d", index)
		if _, err := store.ApplyCanonicalMutation(context.Background(), canonicalRevisionMutation(scope, uint64(index-1), "operation-"+id, canonicalRevision("revision-"+id, "item-"+id))); err != nil {
			t.Fatalf("ApplyCanonicalMutation(%d) error = %v", index, err)
		}
	}
	revisions, err := store.LoadCanonicalRevisions(context.Background(), boyan.CanonicalRevisionReadRequest{Scope: scope, RevisionIDs: []string{"revision-2", stateTestRevisionOneID}})
	if err != nil || len(revisions) != 2 || revisions[0].RevisionID != "revision-2" || revisions[1].RevisionID != stateTestRevisionOneID {
		t.Fatalf("LoadCanonicalRevisions() = %#v, error %v", revisions, err)
	}
	heads, err := store.ScanActiveHeads(context.Background(), boyan.ActiveHeadScanRequest{Scope: scope, Limit: 1})
	if err != nil || len(heads) != 1 || heads[0].ItemID != "item-1" {
		t.Fatalf("ScanActiveHeads(first) = %#v, error %v", heads, err)
	}
	heads, err = store.ScanActiveHeads(context.Background(), boyan.ActiveHeadScanRequest{Scope: scope, AfterItemID: heads[0].ItemID, Limit: 1})
	if err != nil || len(heads) != 1 || heads[0].ItemID != "item-2" {
		t.Fatalf("ScanActiveHeads(next) = %#v, error %v", heads, err)
	}
	active, err := store.ScanActiveMemory(context.Background(), boyan.ActiveMemoryScanRequest{Scope: scope, Limit: 1})
	if err != nil || len(active) != 1 || active[0].Item.ItemID != "item-1" || len(active[0].Evidence) != 1 {
		t.Fatalf("ScanActiveMemory() = %#v, error %v", active, err)
	}
}

func TestCanonicalStoreProjectionManifestFailsClosedUntilActivation(t *testing.T) {
	store, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "memory.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Date(2026, time.August, 6, 0, 0, 0, 0, time.UTC)
	manifest := boyan.ProjectionManifest{Scope: boyan.Scope{Key: "canonical:projection-manifest", Kind: boyan.ScopeKindPersonal}, ProjectionID: "bleve", GenerationID: "generation-1", Status: boyan.ProjectionGenerationBuilding, UpdatedAt: now}
	if err := store.MarkProjectionDirty(context.Background(), manifest); err != nil {
		t.Fatalf("MarkProjectionDirty() error = %v", err)
	}
	stored, found, err := store.LoadProjectionManifest(context.Background(), manifest.Scope, manifest.ProjectionID, manifest.GenerationID)
	if err != nil || !found || stored.Status != boyan.ProjectionGenerationDirty {
		t.Fatalf("LoadProjectionManifest(dirty) = %#v, %t, %v", stored, found, err)
	}
	if err := store.AdvanceProjectionWatermark(context.Background(), manifest, 7, now.Add(time.Second)); err != nil {
		t.Fatalf("AdvanceProjectionWatermark() error = %v", err)
	}
	if err := store.ActivateProjectionGeneration(context.Background(), manifest, now.Add(2*time.Second)); err != nil {
		t.Fatalf("ActivateProjectionGeneration() error = %v", err)
	}
	stored, found, err = store.LoadProjectionManifest(context.Background(), manifest.Scope, manifest.ProjectionID, manifest.GenerationID)
	if err != nil || !found || stored.Status != boyan.ProjectionGenerationActive || stored.Watermark != 7 {
		t.Fatalf("LoadProjectionManifest(active) = %#v, %t, %v", stored, found, err)
	}
	active, found, err := store.LoadActiveProjectionManifest(context.Background(), manifest.Scope, manifest.ProjectionID)
	if err != nil || !found || active.GenerationID != manifest.GenerationID {
		t.Fatalf("LoadActiveProjectionManifest() = %#v, %t, %v", active, found, err)
	}
}

func TestCanonicalStoreAtomicallySwitchesActiveProjectionGeneration(t *testing.T) {
	store, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "memory.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Date(2026, time.August, 6, 0, 0, 0, 0, time.UTC)
	scope := boyan.Scope{Key: "canonical:projection-switch", Kind: boyan.ScopeKindPersonal}
	first := boyan.ProjectionManifest{Scope: scope, ProjectionID: "bleve", GenerationID: "generation-1", Status: boyan.ProjectionGenerationBuilding, UpdatedAt: now}
	if err := store.MarkProjectionDirty(context.Background(), first); err != nil {
		t.Fatalf("MarkProjectionDirty(first) error = %v", err)
	}
	if err := store.ActivateProjectionGeneration(context.Background(), first, now.Add(time.Second)); err != nil {
		t.Fatalf("ActivateProjectionGeneration(first) error = %v", err)
	}
	second := boyan.ProjectionManifest{Scope: scope, ProjectionID: "bleve", GenerationID: "generation-2", Status: boyan.ProjectionGenerationBuilding, UpdatedAt: now.Add(2 * time.Second)}
	if err := store.MarkProjectionDirty(context.Background(), second); err != nil {
		t.Fatalf("MarkProjectionDirty(second) error = %v", err)
	}
	active, found, err := store.LoadActiveProjectionManifest(context.Background(), scope, "bleve")
	if err != nil || !found || active.GenerationID != first.GenerationID {
		t.Fatalf("active before switch = %#v, %t, %v", active, found, err)
	}
	if err := store.ActivateProjectionGeneration(context.Background(), second, now.Add(3*time.Second)); err != nil {
		t.Fatalf("ActivateProjectionGeneration(second) error = %v", err)
	}
	active, found, err = store.LoadActiveProjectionManifest(context.Background(), scope, "bleve")
	if err != nil || !found || active.GenerationID != second.GenerationID {
		t.Fatalf("active after switch = %#v, %t, %v", active, found, err)
	}
	old, found, err := store.LoadProjectionManifest(context.Background(), scope, "bleve", first.GenerationID)
	if err != nil || !found || old.Status != boyan.ProjectionGenerationSuperseded {
		t.Fatalf("old generation = %#v, %t, %v", old, found, err)
	}
}

func TestCanonicalStorePersistsProjectionRetentionAndScopeCheckpoints(t *testing.T) {
	store, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "memory.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	scope := boyan.Scope{Key: "canonical:projection-checkpoint", Kind: boyan.ScopeKindPersonal}
	if err := store.SetProjectionRetentionFloor(ctx, scope, "bleve", 7); err != nil {
		t.Fatalf("SetProjectionRetentionFloor() error = %v", err)
	}
	floor, err := store.LoadProjectionRetentionFloor(ctx, scope, "bleve")
	if err != nil || floor != 7 {
		t.Fatalf("LoadProjectionRetentionFloor() = %d, error %v", floor, err)
	}
	if err := store.SetProjectionRetentionFloor(ctx, scope, "bleve", 6); err == nil {
		t.Fatal("SetProjectionRetentionFloor() accepted a backwards floor")
	}
	checkpoint := boyan.ScopeCheckpoint{
		SchemaVersion: boyan.ScopeCheckpointSchemaVersion,
		Scope:         scope,
		Kind:          boyan.ScopeCheckpointBoundary,
		CheckpointID:  "boundary-1",
		ScopeVersion:  3,
		ChangeSeq:     7,
		OccurredAt:    time.Date(2026, time.August, 6, 1, 0, 0, 0, time.UTC),
		UpdatedAt:     time.Date(2026, time.August, 6, 1, 0, 1, 0, time.UTC),
	}
	if err := store.SaveScopeCheckpoint(ctx, checkpoint); err != nil {
		t.Fatalf("SaveScopeCheckpoint() error = %v", err)
	}
	if err := store.SaveScopeCheckpoint(ctx, checkpoint); err != nil {
		t.Fatalf("SaveScopeCheckpoint(replay) error = %v", err)
	}
	got, found, err := store.LoadScopeCheckpoint(ctx, scope, boyan.ScopeCheckpointBoundary)
	if err != nil || !found || got != checkpoint {
		t.Fatalf("LoadScopeCheckpoint() = %+v, found %v, error %v; want %+v, true", got, found, err, checkpoint)
	}
	checkpoint.ChangeSeq = 6
	if err := store.SaveScopeCheckpoint(ctx, checkpoint); err == nil {
		t.Fatal("SaveScopeCheckpoint() accepted a backwards cursor")
	}
}

func TestCanonicalStoreReopensScopeCheckpoint(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "memory.badger")
	store, err := OpenCanonicalStore(directory)
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	scope := boyan.Scope{Key: "canonical:checkpoint-reopen", Kind: boyan.ScopeKindPersonal}
	checkpoint := boyan.ScopeCheckpoint{
		SchemaVersion: boyan.ScopeCheckpointSchemaVersion,
		Scope:         scope,
		Kind:          boyan.ScopeCheckpointTime,
		CheckpointID:  "timer-1",
		ScopeVersion:  1,
		ChangeSeq:     1,
		OccurredAt:    time.Date(2026, time.August, 6, 2, 0, 0, 0, time.UTC),
		UpdatedAt:     time.Date(2026, time.August, 6, 2, 0, 1, 0, time.UTC),
	}
	if err := store.SaveScopeCheckpoint(context.Background(), checkpoint); err != nil {
		t.Fatalf("SaveScopeCheckpoint() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	store, err = OpenCanonicalStore(directory)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	got, found, err := store.LoadScopeCheckpoint(context.Background(), scope, boyan.ScopeCheckpointTime)
	if err != nil || !found || got != checkpoint {
		t.Fatalf("LoadScopeCheckpoint(after reopen) = %+v, found %v, error %v; want %+v, true", got, found, err, checkpoint)
	}
}

func TestCanonicalStoreFaultBeforeCommitLeavesNoPartialMutationAfterReopen(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "memory.badger")
	store, err := OpenCanonicalStore(directory)
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	scope := boyan.Scope{Key: "canonical:fault", Kind: boyan.ScopeKindPersonal}
	mutation := canonicalRevisionMutation(scope, 0, "operation-fault", canonicalRevision("revision-fault", "item-fault"))
	store.beforeCanonicalMutationCommit = func() error { return errors.New("injected pre-commit fault") }
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err == nil {
		t.Fatal("ApplyCanonicalMutation() error = nil, want injected pre-commit failure")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	store, err = OpenCanonicalStore(directory)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	state, err := store.LoadScopeState(context.Background(), scope)
	if err != nil || state.Version != 0 || state.ChangeSeq != 0 {
		t.Fatalf("LoadScopeState(after failed commit) = %#v, error %v", state, err)
	}
	changes, err := store.ScanScopeChanges(context.Background(), scope, 0, 10)
	if err != nil || len(changes) != 0 {
		t.Fatalf("ScanScopeChanges(after failed commit) = %#v, error %v", changes, err)
	}
	outcome, err := store.ApplyCanonicalMutation(context.Background(), mutation)
	if err != nil || outcome.ScopeVersion != 1 || outcome.ChangeSeq != 1 {
		t.Fatalf("ApplyCanonicalMutation(retry) = %#v, error %v", outcome, err)
	}
}

func TestCanonicalStoreRejectsStaleCASAndAllowsIndependentScopes(t *testing.T) {
	store, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "memory.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	scope := boyan.Scope{Key: "canonical:cas", Kind: boyan.ScopeKindPersonal}
	if _, err := store.ApplyCanonicalMutation(context.Background(), canonicalRevisionMutation(scope, 0, "operation-1", canonicalRevision("revision-1", "item-1"))); err != nil {
		t.Fatalf("first ApplyCanonicalMutation() error = %v", err)
	}
	if _, err := store.ApplyCanonicalMutation(context.Background(), canonicalRevisionMutation(scope, 0, "operation-2", canonicalRevision("revision-2", "item-2"))); err == nil {
		t.Fatal("stale CAS mutation succeeded")
	}
	var group sync.WaitGroup
	errs := make(chan error, 2)
	for _, key := range []string{"canonical:independent-a", "canonical:independent-b"} {
		group.Add(1)
		go func(key string) {
			defer group.Done()
			scope := boyan.Scope{Key: key, Kind: boyan.ScopeKindPersonal}
			_, err := store.ApplyCanonicalMutation(context.Background(), canonicalRevisionMutation(scope, 0, "operation-1", canonicalRevision(key+"-revision", key+"-item")))
			errs <- err
		}(key)
	}
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("independent scope mutation error = %v", err)
		}
	}
}

func TestCanonicalStoreIsolatesEqualIDsAcrossScopes(t *testing.T) {
	store, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "memory.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	for _, scope := range []boyan.Scope{
		{Key: "canonical:scope-a", Kind: boyan.ScopeKindPersonal},
		{Key: "canonical:scope-b", Kind: boyan.ScopeKindPersonal},
	} {
		if _, err := store.ApplyCanonicalMutation(context.Background(), canonicalRevisionMutation(scope, 0, "operation-1", canonicalRevision("revision-1", "item-1"))); err != nil {
			t.Fatalf("ApplyCanonicalMutation(%q) error = %v", scope.Key, err)
		}
	}
}

func TestCanonicalStoreErrorMapsTxnTooBig(t *testing.T) {
	err := canonicalStoreError("test", badger.ErrTxnTooBig)
	var memoryErr *boyan.Error
	if !errors.As(err, &memoryErr) || memoryErr.Code != boyan.CodeLimitExceeded {
		t.Fatalf("canonicalStoreError() = %#v", err)
	}
}

func canonicalRevisionMutation(scope boyan.Scope, expectedVersion uint64, operationID string, revision boyan.MemoryRevision) boyan.CanonicalMutation {
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
		SchemaVersion: boyan.MemorySchemaVersionV2,
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
