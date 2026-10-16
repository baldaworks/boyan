//go:build !unix

package sqlite

import (
	"fmt"
	"os"
)

// The repository's evidenced target is linux/amd64. Do not silently claim
// equivalent private ownership validation on an unevidenced platform.
func validateDirectory(_ os.FileInfo, _ bool, _ uint32) error {
	return fmt.Errorf("%w: unsupported ownership platform", ErrUnsafePath)
}

func filesystemRootOwner(_ *os.Root) (uint32, error) {
	return 0, fmt.Errorf("%w: unsupported ownership platform", ErrUnsafePath)
}
func validateFile(_ os.FileInfo) error {
	return fmt.Errorf("%w: unsupported ownership platform", ErrUnsafePath)
}
