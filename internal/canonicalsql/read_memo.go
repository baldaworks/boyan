package canonicalsql

import (
	"bytes"
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

const readMemoEntries = 64

type readMemoEntry struct {
	encoded []byte
	missing bool
}

// Each Tx owns one immutable read snapshot. Reusing its fully checked points
// does not reuse authority across the fresh final reader barrier or any writer.
// Capacity only limits reuse: every uncached key still follows the normal path.
func (tx *Tx) readSnapshotPoint(ctx context.Context, key Key, def familyDef, values [][]byte) ([]byte, error) {
	identity := string(key.Family) + "\x00" + string(stampKey(values))
	if entry, found := tx.readMemo[identity]; found {
		if entry.missing {
			return nil, memory.PermanentError(memory.CodeNotFound, "canonical SQL row not found", nil)
		}

		return bytes.Clone(entry.encoded), nil
	}

	encoded, err := tx.getPoint(ctx, key, def, values)
	missing := false

	if err != nil {
		code, _, classified := memory.ClassifyError(err)
		if !classified || code != memory.CodeNotFound {
			return nil, err
		}

		missing = true
	}

	retained := len(identity) + len(encoded)
	if len(tx.readMemo) < readMemoEntries && retained <= MaxEnvelopeBytes-tx.readBytes {
		if tx.readMemo == nil {
			tx.readMemo = make(map[string]readMemoEntry)
		}

		tx.readMemo[identity] = readMemoEntry{encoded: bytes.Clone(encoded), missing: missing}
		tx.readBytes += retained
	}

	return bytes.Clone(encoded), err
}
