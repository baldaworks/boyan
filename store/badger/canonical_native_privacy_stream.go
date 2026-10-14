package badger

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
	"github.com/dgraph-io/badger/v4/pb"
	"google.golang.org/protobuf/proto"
)

// This operational limit is deliberately finite; upstream batches can overshoot
// their soft limit. It is checked on the uint64 header before any allocation.
const maxNativePrivacyFrameBytes = 256 << 20

func readNativePrivacyFrame(ctx context.Context, reader io.Reader) (*pb.KVList, error) {
	if err := canonicalContextError(ctx); err != nil {
		return nil, err
	}

	var size uint64
	if err := binary.Read(reader, binary.LittleEndian, &size); err != nil {
		return nil, err
	}

	if size > maxNativePrivacyFrameBytes {
		return nil, memory.PermanentError(memory.CodeLimitExceeded, "native backup frame exceeds bound", nil)
	}

	encoded := make([]byte, int(size))
	if _, err := io.ReadFull(reader, encoded); err != nil {
		return nil, err
	}

	if err := canonicalContextError(ctx); err != nil {
		return nil, err
	}

	var list pb.KVList
	if err := proto.Unmarshal(encoded, &list); err != nil {
		return nil, invalidSourcePrivacy()
	}

	return &list, nil
}

func writeNativePrivacyFrame(writer io.Writer, list *pb.KVList) error {
	if proto.Size(list) > maxNativePrivacyFrameBytes {
		return memory.PermanentError(memory.CodeLimitExceeded, "native backup frame exceeds bound", nil)
	}

	encoded, err := proto.Marshal(list)
	if err != nil {
		return err
	}

	if len(encoded) > maxNativePrivacyFrameBytes {
		return memory.PermanentError(memory.CodeLimitExceeded, "native backup frame exceeds bound", nil)
	}

	if err := binary.Write(writer, binary.LittleEndian, uint64(len(encoded))); err != nil {
		return err
	}

	n, err := writer.Write(encoded)
	if err == nil && n != len(encoded) {
		return io.ErrShortWrite
	}

	return err
}

func (s *CanonicalStore) stageNativePrivacyBackup(
	ctx context.Context, source io.Reader,
) (file *os.File, resultErr error) {
	file, err := os.CreateTemp(s.db.Opts().Dir, ".boyan-native-restore-*")
	if err != nil {
		return nil, err
	}
	// An unlinked open spool cannot survive a process crash or become recovery authority.
	if err := os.Remove(file.Name()); err != nil {
		_ = file.Close()

		return nil, err
	}

	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, file.Close())
		}
	}()

	if err := s.stageNativePrivacyFrames(ctx, source, file); err != nil {
		return file, err
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return file, err
	}

	return file, nil
}

func (s *CanonicalStore) loadNativePrivacyBackup(
	ctx context.Context, source io.Reader, pending int, loadingRetry bool,
) (resultErr error) {
	if err := s.db.View(func(txn *badger.Txn) error {
		return verifyCanonicalIntegrityMode(ctx, txn, !loadingRetry)
	}); err != nil {
		return err
	}

	spool, err := s.stageNativePrivacyBackup(ctx, source)
	if err != nil {
		return err
	}

	defer func() { resultErr = errors.Join(resultErr, spool.Close()) }()

	if err := canonicalContextError(ctx); err != nil {
		return err
	}
	// Load alone owns Badger during this call; no transaction or lookup runs in its reader.
	if err := s.db.Load(spool, pending); err != nil {
		return err
	}

	if s.afterNativeRestoreLoad != nil {
		if err := s.afterNativeRestoreLoad(); err != nil {
			return err
		}
	}

	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		return err
	}

	return s.replayNativePrivacyBackup(ctx, spool)
}

func (s *CanonicalStore) stageNativePrivacyFrames(ctx context.Context, source io.Reader, destination io.Writer) error {
	frames := 0

	for {
		list, err := readNativePrivacyFrame(ctx, source)
		if errors.Is(err, io.EOF) {
			if frames == 0 {
				return invalidSourcePrivacy()
			}

			return nil
		}

		if err != nil {
			return err
		}

		if err := s.stageNativePrivacyFrame(list); err != nil {
			return err
		}

		if err := writeNativePrivacyFrame(destination, list); err != nil {
			return err
		}

		frames++
	}
}

func (s *CanonicalStore) stageNativePrivacyFrame(list *pb.KVList) error {
	kept := list.Kv[:0]
	for _, kv := range list.Kv {
		retain, err := s.stageNativePrivacyKV(kv)
		if err != nil {
			return err
		}

		if retain {
			kept = append(kept, kv)
		}
	}

	list.Kv = kept

	return nil
}
