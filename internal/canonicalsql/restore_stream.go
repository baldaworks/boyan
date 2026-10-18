package canonicalsql

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/archive"
)

// A valid single record fits well below the opaque operational cap. This
// tighter bound covers base64 envelope and all four maximum tuple components.
const maxRestoreJSONBytes = 8 << 20

func privateRestoreSpool() (*os.File, error) {
	f, err := os.CreateTemp("", "boyan-sql-restore-*")
	if err != nil {
		return nil, err
	}

	if err := os.Remove(f.Name()); err != nil {
		return nil, errors.Join(err, f.Close())
	}

	return f, nil
}

func readRestoreFrame(ctx context.Context, r io.Reader) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var prefix [4]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		return nil, err
	}

	n := binary.BigEndian.Uint32(prefix[:])
	if n > archive.MaxOpaqueFrameBytes || n > maxRestoreJSONBytes {
		return nil, limitError()
	}

	if n == 0 {
		return nil, invalid("empty canonical archive frame")
	}

	data := make([]byte, int(n))
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}

	return data, ctx.Err()
}

//nolint:cyclop,gocognit,funlen // Stream checks and unlinked resources share one error scope.
func stageRestore(
	ctx context.Context,
	r io.Reader,
	postgres bool,
) (file *os.File, header archive.BackupHeader, digest string, result error) {
	file, err := privateRestoreSpool()
	if err != nil {
		return nil, header, "", err
	}
	defer func() {
		if result != nil {
			result = errors.Join(result, file.Close())
		}
	}()

	index, err := privateRestoreSpool()
	if err != nil {
		return file, header, "", err
	}

	defer func() { result = errors.Join(result, index.Close()) }()

	keys := restoreKeyIndex{file: index}

	data, err := readRestoreFrame(ctx, r)
	if err != nil {
		return file, header, "", err
	}

	if err := decodeRestoreJSON(data, &header); err != nil {
		return file, header, "", invalid("invalid canonical archive header")
	}

	if err := validateRestoreHeader(header, postgres); err != nil {
		return file, header, "", err
	}

	h := sha256.New()

	write := func(w io.Writer, data []byte) error {
		var prefix [4]byte
		binary.BigEndian.PutUint32(prefix[:], uint32(len(data)))

		for _, part := range [][]byte{prefix[:], data} {
			n, err := w.Write(part)
			if err != nil {
				return err
			}

			if n != len(part) {
				return io.ErrShortWrite
			}
		}

		return nil
	}
	if err := write(h, data); err != nil {
		return file, header, "", err
	}

	var (
		count    uint64
		previous *StampCursor
	)

	for {
		data, err = readRestoreFrame(ctx, r)
		if err != nil {
			return file, header, "", err
		}

		var kind struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal(data, &kind) != nil {
			return file, header, "", invalid("invalid canonical archive frame")
		}

		if kind.Kind == "footer" {
			var footer archive.BackupFooter
			if decodeRestoreJSON(data, &footer) != nil || footer.RecordCount != count ||
				footer.SHA256 != hex.EncodeToString(h.Sum(nil)) {
				return file, header, "", invalid("invalid canonical archive checksum or count")
			}

			var extra [1]byte
			if n, err := io.ReadFull(r, extra[:]); n != 0 || !errors.Is(err, io.EOF) {
				return file, header, "", invalid("trailing canonical archive data")
			}

			if _, err := file.Seek(0, io.SeekStart); err != nil {
				return file, header, "", err
			}

			return file, header, footer.SHA256, nil
		}

		record, key, err := decodeRestoreRecord(data)
		if err != nil {
			return file, header, "", err
		}

		_, values, _ := key.values()

		cursor := StampCursor{
			Sequence:  record.Sequence,
			Family:    key.Family,
			BinaryKey: stampKey(values),
		}
		if record.Sequence == 0 || record.Sequence > header.End ||
			(header.Mode == "incremental" && record.Sequence <= header.Base) ||
			(previous != nil && !restoreCursorAfter(cursor, *previous)) {
			return file, header, "", invalid("invalid canonical archive record order")
		}

		if err := keys.add(
			ctx,
			stampKey(append([][]byte{[]byte(key.Family)}, values...)),
		); err != nil {
			return file, header, "", err
		}

		previous = &cursor

		if err := write(io.MultiWriter(file, h), data); err != nil {
			return file, header, "", err
		}

		count++
	}
}

//nolint:cyclop // Check the supported archive identity and versions together.
func validateRestoreHeader(h archive.BackupHeader, postgres bool) error {
	backend := "sqlite"
	if postgres {
		backend = "postgres"
	}

	if len(h.SourceStoreUUID) != 2*storeIdentityBytes {
		return invalid("invalid canonical archive identity")
	}

	id, err := hex.DecodeString(h.SourceStoreUUID)
	if err != nil || len(id) != storeIdentityBytes || h.Kind != "header" || h.Format != archive.SQLBackupFormat ||
		h.Backend != backend || h.SchemaVersion != 1 || h.PrivacyProtocol != 1 || h.ProofProtocol != 1 ||
		h.End < h.Base || (h.Mode != "full" && h.Mode != "incremental") ||
		(h.Mode == "full" && h.Base != 0) ||
		(h.Mode == "incremental" && h.Base == 0) {
		return invalid("unsupported canonical archive identity or version")
	}

	return nil
}

func restoreCursorAfter(a, b StampCursor) bool {
	if a.Sequence != b.Sequence {
		return a.Sequence > b.Sequence
	}

	if a.Family != b.Family {
		return a.Family > b.Family
	}

	return bytes.Compare(a.BinaryKey, b.BinaryKey) > 0
}

//nolint:cyclop,funlen // Ordered size, tuple, envelope and proof checks precede replay.
func decodeRestoreRecord(data []byte) (archive.BackupRecord, Key, error) {
	// Keep the base64 value encoded until its decoded length has been bounded.
	var (
		wire struct {
			Kind       string          `json:"kind"`
			Family     string          `json:"family"`
			Components json.RawMessage `json:"components"`
			Sequence   uint64          `json:"sequence"`
			Deleted    bool            `json:"deleted"`
			Envelope   string          `json:"envelope,omitempty"`
		}
		record archive.BackupRecord
	)
	if decodeRestoreJSON(data, &wire) != nil || wire.Kind != "record" {
		return record, Key{}, invalid("invalid canonical archive record")
	}

	if decodedRestoreSize(wire.Envelope) > MaxEnvelopeBytes {
		return record, Key{}, limitError()
	}

	value, err := base64.StdEncoding.DecodeString(wire.Envelope)
	if err != nil || len(value) > MaxEnvelopeBytes {
		return record, Key{}, invalid("invalid canonical archive value")
	}

	components, err := decodeRestoreComponents(Family(wire.Family), wire.Components)
	if err != nil {
		return record, Key{}, err
	}

	key, err := keyFromFrame(Family(wire.Family), stampKey(components))
	if err != nil {
		return record, key, err
	}

	record = archive.BackupRecord{
		Kind:       wire.Kind,
		Family:     wire.Family,
		Components: components,
		Sequence:   wire.Sequence,
		Deleted:    wire.Deleted,
		Envelope:   value,
	}
	if wire.Deleted {
		if len(value) != 0 {
			return record, key, invalid("canonical tombstone carries value")
		}

		return record, key, nil
	}

	if _, err := columnValues(key, value); err != nil {
		return record, key, err
	}

	envelope, err := memory.UnmarshalRecordEnvelope(value)
	if err != nil {
		return record, key, err
	}

	if err := validateRestoreJSON(envelope.Payload); err != nil {
		return record, key, err
	}

	if err := validateCanonicalProofRecord(archiveComponents(key), envelope); err != nil {
		return record, key, err
	}

	if key.Family == TurnCertificate {
		if _, err := decodePrivacyCertificate(envelope.Payload, string(key.Parts[0])); err != nil {
			return record, key, err
		}
	}

	return record, key, nil
}

func decodedRestoreSize(value string) int {
	n := base64.StdEncoding.DecodedLen(len(value))
	if len(value) > 0 && value[len(value)-1] == '=' {
		n--
	}

	if len(value) > 1 && value[len(value)-2] == '=' {
		n--
	}

	return n
}

//nolint:cyclop // Bound component count and decoded size before each base64 allocation.
func decodeRestoreComponents(f Family, data []byte) ([][]byte, error) {
	def, err := familyDefinition(f)
	if err != nil {
		return nil, err
	}

	d := json.NewDecoder(bytes.NewReader(data))

	token, err := d.Token()
	if err != nil || token != json.Delim('[') {
		return nil, invalid("invalid canonical archive components")
	}

	parts := make([][]byte, 0, len(def.columns()))
	for d.More() {
		if len(parts) == cap(parts) {
			return nil, invalid("invalid canonical archive arity")
		}

		var encoded string
		if err := d.Decode(&encoded); err != nil {
			return nil, invalid("invalid canonical archive component")
		}

		if decodedRestoreSize(encoded) > math.MaxUint16 {
			return nil, limitError()
		}

		part, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, invalid("invalid canonical archive component")
		}

		parts = append(parts, part)
	}

	if _, err := d.Token(); err != nil || len(parts) != cap(parts) {
		return nil, invalid("invalid canonical archive arity")
	}

	return parts, nil
}

func archiveComponents(key Key) []string {
	components := []string{string(key.Family)}

	def, _, _ := key.values()
	if def.scoped {
		components = append(components, key.Scope.Key, string(key.Scope.Kind))
	}

	for _, part := range key.Parts {
		components = append(components, string(part))
	}

	return components
}
