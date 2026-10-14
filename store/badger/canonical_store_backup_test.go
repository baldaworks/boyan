package badger_test

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

	source := openCanonicalTestStore(t, sourceDir)

	scope := boyan.Scope{Key: "canonical:backup", Kind: boyan.ScopeKindPersonal}

	mutation := canonicalRevisionMutation(
		scope,
		0,
		"operation-backup",
		canonicalRevision("revision-backup", "item-backup"),
	)
	if _, err := source.ApplyCanonicalMutation(ctx, mutation); err != nil {
		t.Fatalf("ApplyCanonicalMutation() error = %v", err)
	}

	var backup bytes.Buffer

	version, err := source.WriteCanonicalBackup(ctx, &backup, 0)
	if err != nil || version == 0 || backup.Len() == 0 {
		t.Fatalf("WriteCanonicalBackup() version=%d bytes=%d error=%v", version, backup.Len(), err)
	}

	closeCanonicalTestStore(t, source)

	restored := openCanonicalTestStore(t, filepath.Join(t.TempDir(), "restored.badger"))

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

func TestCanonicalMaintenanceRejectsTypedNilStreams(t *testing.T) {
	store := openCanonicalTestStore(t, filepath.Join(t.TempDir(), "typed-nil.badger"))
	t.Cleanup(func() { _ = store.Close() })

	var stream *bytes.Buffer

	tests := []struct {
		name string
		call func() error
	}{
		{"backup", func() error {
			_, err := store.WriteCanonicalBackup(context.Background(), stream, 0)

			return err
		}},
		{"restore", func() error {
			return store.RestoreCanonicalBackup(context.Background(), stream, 1)
		}},
		{"logical export", func() error {
			return store.ExportCanonicalLogical(context.Background(), stream)
		}},
		{"logical import", func() error {
			return store.ImportCanonicalLogical(context.Background(), stream)
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("maintenance call panicked: %v", recovered)
				}
			}()

			if err := tc.call(); err == nil {
				t.Fatal("maintenance call error = nil, want typed-nil rejection")
			}
		})
	}
}

func TestCanonicalRestoreRejectsEmptyNativeStream(t *testing.T) {
	store := openCanonicalTestStore(t, filepath.Join(t.TempDir(), "empty-restore.badger"))
	t.Cleanup(func() { _ = store.Close() })

	if err := store.RestoreCanonicalBackup(context.Background(), bytes.NewReader(nil), 1); err == nil {
		t.Fatal("RestoreCanonicalBackup(empty) error = nil, want invalid-backup failure")
	}
}
