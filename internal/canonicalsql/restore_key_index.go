package canonicalsql

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"io"
	"os"
)

// The spool index bounds resident key memory regardless of artifact size.
// Hash buckets only select disk candidates; exact original framing decides
// duplicates, including distinct keys with the same hash. Offsets are private
// to this unlinked, disposable file and can never become recovery authority.
type restoreKeyIndex struct {
	file  *os.File
	heads [65536]int64
}

func (s *restoreKeyIndex) add(ctx context.Context, key []byte) error {
	hash := sha256.Sum256(key)

	bucket := binary.BigEndian.Uint16(hash[:2])
	for offset := s.heads[bucket]; offset != 0; {
		if err := ctx.Err(); err != nil {
			return err
		}

		var header [12]byte
		if _, err := s.file.ReadAt(header[:], offset-1); err != nil {
			return err
		}

		n := binary.BigEndian.Uint32(header[8:])
		if int(n) == len(key) {
			stored := make([]byte, len(key))
			if _, err := s.file.ReadAt(stored, offset-1+int64(len(header))); err != nil {
				return err
			}

			if bytes.Equal(stored, key) {
				return invalid("duplicate canonical archive identity")
			}
		}

		offset = int64(binary.BigEndian.Uint64(header[:8]))
	}

	offset, err := s.file.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}

	var header [12]byte
	binary.BigEndian.PutUint64(header[:8], uint64(s.heads[bucket]))
	binary.BigEndian.PutUint32(header[8:], uint32(len(key)))

	if _, err := s.file.Write(header[:]); err != nil {
		return err
	}

	if _, err := s.file.Write(key); err != nil {
		return err
	}

	s.heads[bucket] = offset + 1

	return nil
}
