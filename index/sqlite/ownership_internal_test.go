//go:build unix

package sqlite

import (
	"os"
	"syscall"
	"testing"
)

type ownedDirectoryFixture struct {
	os.FileInfo

	owner uint32
}

func (fixture ownedDirectoryFixture) Sys() any {
	stat := *fixture.FileInfo.Sys().(*syscall.Stat_t)
	stat.Uid = fixture.owner

	return &stat
}

// Remapping system root must not authorize a foreign owner of private state.
func TestMappedSystemOwnershipKeepsPrivateOwnerStrict(t *testing.T) {
	t.Parallel()

	system, err := os.Stat("/")
	if err != nil {
		t.Fatal(err)
	}

	private, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	current := uint32(os.Geteuid())

	mapped := current + 1
	if err := validateDirectory(ownedDirectoryFixture{system, mapped}, false, mapped); err != nil {
		t.Fatalf("mapped system ancestor rejected: %v", err)
	}

	if err := validateDirectory(ownedDirectoryFixture{private, mapped}, true, mapped); err == nil {
		t.Fatal("mapped system owner accepted as private state owner")
	}

	if err := validateDirectory(ownedDirectoryFixture{system, mapped + 1}, false, mapped); err == nil {
		t.Fatal("unrelated ancestor owner accepted")
	}
}
