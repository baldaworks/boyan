//go:build integration

package canonicalsql

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// A process-local mutex cannot provide ownership. The child must observe the
// same kernel lock without sharing this process's heap or file descriptors.
func TestSQLiteOwnerExcludesOtherProcess(t *testing.T) {
	const childPath = "BOYAN_SQLITE_OWNER_CHILD_PATH"
	if path := os.Getenv(childPath); path != "" {
		owner, err := OpenSQLiteOwner(t.Context(), SQLiteConfig{Path: path})
		if owner != nil {
			_ = owner.Close()

			t.Fatal("another process acquired canonical ownership")
		}

		assertCode(t, err, memory.CodeUnavailable)

		return
	}

	path := filepath.Join(t.TempDir(), "canonical.sqlite")
	openTestOwner(t, path)

	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	command := exec.CommandContext(t.Context(), binary, "-test.run=^TestSQLiteOwnerExcludesOtherProcess$", "-test.v")

	command.Env = append(os.Environ(), childPath+"="+path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("owner child: %v\n%s", err, output)
	}
}
