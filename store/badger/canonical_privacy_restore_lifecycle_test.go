package badger //nolint:testpackage // Exercises cancellation and durable restore-fence phases.

import (
	"bytes"
	"context"
	"errors"
	"testing"

	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestPrivacyRestoreLoadingAndValidatedRestart(t *testing.T) {
	for _, kind := range []string{"after-load", "replay", "preparation", "sync-loading", "sync-validated", "fence"} {
		t.Run(kind, func(t *testing.T) {
			source, mutation := privacyRestoreLifecycleSource(t, kind)
			encoded := nativePrivacyBackup(t, source, 0)
			dir := t.TempDir()

			destination, err := OpenCanonicalStore(dir)
			if err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			installPrivacyRestoreFailure(t, destination, kind, cancel)

			if err := destination.RestoreCanonicalBackup(ctx, bytes.NewReader(encoded), 1); err == nil {
				t.Fatal("restore failure ignored")
			}

			assertRestoreUnavailable(t, destination, mutation)
			assertNoNativePrivacySpool(t, destination)

			if err := destination.Close(); err != nil {
				t.Fatal(err)
			}

			reopened, err := OpenCanonicalStore(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()

			assertRestoreUnavailable(t, reopened, mutation)
			assertPrivacyRestoreRestart(t, reopened, mutation, kind, encoded)
		})
	}
}

func privacyRestoreLifecycleSource(t *testing.T, kind string) (*CanonicalStore, m.CanonicalMutation) {
	t.Helper()

	source, mutation := privacyFixture(t)
	if kind == "preparation" {
		certifyPrivacyHistory(t, &mutation)
	}

	if _, err := source.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}

	if kind == "after-load" {
		if err := source.db.Update(publishSourcePrivacyReady); err != nil {
			t.Fatal(err)
		}
	}

	return source, mutation
}

func installPrivacyRestoreFailure(t *testing.T, store *CanonicalStore, kind string, cancel context.CancelFunc) {
	t.Helper()

	switch kind {
	case "after-load":
		store.afterNativeRestoreLoad = func() error {
			if err := store.db.View(requireSourcePrivacyReady); err != nil {
				t.Errorf("higher-MVCC copied readiness is not present at Load seam: %v", err)
			}

			cancel()

			return nil
		}
	case "replay":
		store.afterNativeRestoreReplay = func() error {
			cancel()

			return nil
		}
	case "preparation":
		store.afterSourcePrivacyBatch = func(string, int) error {
			cancel()

			return nil
		}
	case "sync-loading", "sync-validated":
		store.beforeRestoreDBSync = privacyRestoreSyncFault(kind)
	case "fence":
		store.beforeRestoreFenceWrite = func(phase string) error {
			if phase == restoreValidated {
				return errors.New("fence fault")
			}

			return nil
		}
	}
}

func assertPrivacyRestoreRestart(
	t *testing.T, reopened *CanonicalStore, mutation m.CanonicalMutation, kind string, encoded []byte,
) {
	t.Helper()

	err := reopened.PrepareSourcePrivacy(context.Background())

	validated := kind == "preparation" || kind == "sync-validated"
	if validated {
		if err != nil {
			t.Fatalf("validated prep restart failed: %v", err)
		}

		assertPrivacyRestoredOutcome(t, reopened, mutation, "native-standard")
	} else {
		if err == nil {
			t.Fatal("unvalidated loading fence prepared")
		}

		if err := applyPrivacyRestore(reopened, false, encoded); err != nil {
			t.Fatalf("complete retry failed: %v", err)
		}

		assertPrivacyRestoredOutcome(t, reopened, mutation, "native-standard")
	}
}

func privacyRestoreSyncFault(kind string) func() error {
	count := 0

	return func() error {
		count++
		if count == 1 && kind == "sync-loading" || count == 2 && kind == "sync-validated" {
			return errors.New("sync fault")
		}

		return nil
	}
}
