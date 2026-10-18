package archive_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
)

type backupStore interface {
	WriteCanonicalBackup(ctx context.Context, destination io.Writer, since uint64) (uint64, error)
	VerifyCanonicalIntegrity(ctx context.Context) error
}

func TestSQLBackupPublicContract(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		s, ok := f.Store.(backupStore)
		if !ok {
			t.Fatal("SQL owner lacks backup/integrity contract")
		}

		if err := s.VerifyCanonicalIntegrity(t.Context()); err != nil {
			t.Fatal(err)
		}

		var out bytes.Buffer

		cursor, err := s.WriteCanonicalBackup(t.Context(), &out, 0)
		if err != nil {
			t.Fatal(err)
		}

		frames := decodeArchive(t, out.Bytes())
		assertEmptyHeader(t, frames[0])

		if len(frames) != 2 || frames[0]["format"] != "boyan-sql-backup/v1" ||
			frames[0]["backend"] != string(f.Backend) ||
			cursor != 0 {
			t.Fatalf("empty archive: %v cursor=%d", frames, cursor)
		}
	})
}

func assertEmptyHeader(t *testing.T, header map[string]any) {
	t.Helper()

	for key, want := range map[string]any{
		"schema_version": float64(1), "privacy_protocol": float64(1), "proof_protocol": float64(1),
		"mode": "full", "base": float64(0), "end": float64(0),
	} {
		if header[key] != want {
			t.Errorf("header %s=%v want %v", key, header[key], want)
		}
	}

	id, ok := header["source_store_uuid"].(string)
	if !ok || len(id) != 32 {
		t.Fatal("missing stable source identity")
	}
}

// Decode independently: four-byte unsigned length followed by JSON, with SHA
// covering the header and record length prefixes and bytes (excluding footer).
//
//nolint:cyclop // Independent framing, truncation and checksum assertions stay together.
func decodeArchive(t *testing.T, data []byte) []map[string]any {
	t.Helper()

	var frames []map[string]any

	h := sha256.New()

	for len(data) != 0 {
		if len(data) < 4 {
			t.Fatal("truncated length")
		}

		n := int(binary.BigEndian.Uint32(data[:4]))
		if n > 256*1024*1024 || n > len(data)-4 {
			t.Fatal("invalid frame length")
		}

		frame := data[:4+n]

		var obj map[string]any
		if err := json.Unmarshal(frame[4:], &obj); err != nil {
			t.Fatal(err)
		}

		data = data[4+n:]
		if obj["kind"] == "footer" {
			if len(data) != 0 || obj["sha256"] != hex.EncodeToString(h.Sum(nil)) ||
				obj["record_count"] != float64(len(frames)-1) {
				t.Fatal("invalid exact-byte footer")
			}
		} else {
			_, _ = h.Write(frame)
		}

		frames = append(frames, obj)
	}

	if len(frames) < 2 || frames[len(frames)-1]["kind"] != "footer" {
		t.Fatal("missing footer")
	}

	return frames
}
