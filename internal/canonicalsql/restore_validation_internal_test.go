package canonicalsql

import (
	"bytes"
	"context"
	"crypto/sha256"
	"strings"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/archive"
)

//nolint:cyclop,funlen // Check valid and invalid validation rollback over the complete fixed-family inventory.
func TestRestoreValidationRollbackAllFamilies(t *testing.T) {
	forEachOwner(t, func(t *testing.T, o *Owner, _ func() *Owner) {
		key := Key{Family: Payload, Parts: [][]byte{[]byte("retained-local")}}

		value := envelope(t, Payload, "retained")
		if err := o.Update(
			t.Context(),
			func(ctx context.Context, tx *Tx) error { return tx.Put(ctx, key, value) },
		); err != nil {
			t.Fatal(err)
		}

		ctx, release, err := o.admit(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer release()

		if err := o.restoreTransaction(ctx, true, func(ctx context.Context, tx *Tx) error {
			return tx.writeRestoreControl(
				ctx,
				"restore_control",
				restoreControl{Version: 1, Phase: "loading"},
			)
		}); err != nil {
			t.Fatal(err)
		}

		var records bytes.Buffer

		writer := backupWriter{destination: &records, digest: sha256.New()}

		for _, shape := range expectedShapes {
			k := familyFixtureKey(t, shape.family)

			_, parts, err := k.values()
			if err != nil {
				t.Fatal(err)
			}

			if err := writer.frame(
				ctx,
				archive.BackupRecord{
					Kind:       "record",
					Family:     string(k.Family),
					Components: parts,
					Sequence:   1,
					Deleted:    true,
				},
				false,
			); err != nil {
				t.Fatal(err)
			}
		}

		if err := o.validateRestoreInput(ctx, bytes.NewReader(records.Bytes())); err != nil {
			t.Fatal(err)
		}

		assertRestoreValidationUnchanged(t, o, ctx, key, value)

		orphan := sourcePrivacy{
			Version:         1,
			CoverageVersion: 1,
			Scope:           memory.Scope{Key: "test:orphan", Kind: memory.ScopeKindPersonal},
			SourceID:        "orphan",
			Payload: memory.PayloadRef{
				ID:       "missing",
				Digest:   strings.Repeat("a", 64),
				ByteSize: 1,
			},
			State:     sourcePrivacyUnknown,
			Preparing: true,
		}

		encoded, err := Encode(SourcePrivacy, orphan)
		if err != nil {
			t.Fatal(err)
		}

		if err := writer.frame(
			ctx,
			archive.BackupRecord{
				Kind:   "record",
				Family: string(SourcePrivacy),
				Components: [][]byte{
					[]byte(orphan.Scope.Key),
					[]byte(orphan.Scope.Kind),
					[]byte(orphan.SourceID),
				},
				Sequence: 2,
				Envelope: encoded,
			},
			false,
		); err != nil {
			t.Fatal(err)
		}

		if err := o.validateRestoreInput(ctx, bytes.NewReader(records.Bytes())); err == nil {
			t.Fatal("orphan binding validated")
		}

		assertRestoreValidationUnchanged(t, o, ctx, key, value)
	})
}

//nolint:cyclop // Check all fixed families, exact value, stamps, clock and durable loading control.
func assertRestoreValidationUnchanged(
	t *testing.T,
	o *Owner,
	ctx context.Context,
	key Key,
	value []byte,
) {
	t.Helper()
	assertClock(t, o.connection, 1, 1)

	if err := o.restoreTransaction(ctx, false, func(ctx context.Context, tx *Tx) error {
		for _, shape := range expectedShapes {
			page, err := tx.Scan(ctx, Range{Family: shape.family, Limit: 2})
			if err != nil {
				return err
			}

			want := 0
			if shape.family == Payload {
				want = 1
			}

			if len(page) != want {
				t.Fatalf("validation committed family %s", shape.family)
			}
		}

		got, err := tx.Get(ctx, key)
		if err != nil {
			return err
		}

		if !bytes.Equal(got, value) {
			t.Fatal("validation replaced destination value")
		}

		control, err := tx.readRestoreControl(ctx, "restore_control")
		if err != nil {
			return err
		}

		if control == nil || control.Phase != "loading" || control.Header != nil {
			t.Fatal("validation trusted or cleared control")
		}

		return tx.checkIntegrity(ctx, 1)
	}); err != nil {
		t.Fatal(err)
	}
}
