package badger

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

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
