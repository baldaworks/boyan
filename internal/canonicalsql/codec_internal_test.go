package canonicalsql

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"math"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/jackc/pgx/v5/pgconn"
)

// Catches truncated unsigned columns, wrapping arithmetic, and codec validation
// that accepts wrong-family, malformed or checksum-tampered values.
func TestUnsignedValidation(t *testing.T) {
	for _, value := range []uint64{0, math.MaxInt64, math.MaxInt64 + 1, math.MaxUint64} {
		got, err := DecodeUint64(Uint64(value))
		if err != nil || got != value {
			t.Fatal("uint64 encoding lost precision")
		}
	}

	for _, size := range []int{0, 1, 7, 9} {
		if _, err := DecodeUint64(make([]byte, size)); err == nil {
			t.Fatal("malformed length accepted")
		}
	}

	if _, err := NextUint64(math.MaxUint64); err == nil {
		t.Fatal("unsigned overflow wrapped")
	}
}

func TestConnectionResourceErrorsAreRetryable(t *testing.T) {
	for _, cause := range []error{sql.ErrConnDone, driver.ErrBadConn, pgconn.ErrConnClosed} {
		err := databaseError(t.Context(), "read", cause)
		if !memory.IsRetryable(err) {
			t.Fatalf("connection resource error is permanent: %v", err)
		}
	}
}

func TestOversizedStoredEnvelopeFailsClosed(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		scope := testScope()
		key := Key{Family: Source, Scope: scope, Parts: [][]byte{[]byte("id")}}

		mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), key, envelope(t, Source, true)) })

		if db.SQLite != nil {
			rawExec(t, c, "PRAGMA ignore_check_constraints=ON")
		} else {
			rawExec(t, c, "ALTER TABLE boyan_canonical.source DROP CONSTRAINT source_envelope_check")
		}

		rawExec(t, c, "UPDATE "+c.table("source")+" SET envelope=$1", bytes.Repeat([]byte{1}, MaxEnvelopeBytes+1))

		if db.SQLite != nil {
			rawExec(t, c, "PRAGMA ignore_check_constraints=OFF")
		}

		err := c.View(t.Context(), func(tx *Tx) error {
			_, err := tx.Get(t.Context(), key)

			return err
		})
		assertCode(t, err, memory.CodeLimitExceeded)
		err = c.View(t.Context(), func(tx *Tx) error {
			_, err := tx.Scan(t.Context(), Range{Family: Source, Scope: &scope, Limit: 1})

			return err
		})
		assertCode(t, err, memory.CodeLimitExceeded)
	})
}

func TestEnvelopeValidation(t *testing.T) {
	oversized := bytes.Repeat([]byte{1}, MaxEnvelopeBytes+1)
	for _, bad := range [][]byte{nil, []byte("invalid"), envelope(t, Payload, true), oversized} {
		if err := Decode(Source, bad, new(any)); err == nil {
			t.Fatal("invalid envelope accepted")
		}
	}

	tampered := envelope(t, Source, "payload")

	tampered[len(tampered)-1] ^= 1
	if err := Decode(Source, tampered, new(any)); err == nil {
		t.Fatal("tampered envelope accepted")
	}

	if _, err := Encode(Source, make(chan int)); err == nil {
		t.Fatal("unsupported payload accepted")
	}

	if _, err := Encode(Source, bytes.Repeat([]byte{1}, MaxEnvelopeBytes)); err == nil {
		t.Fatal("oversized encoded payload accepted")
	}
}

// Catches read paths returning corrupt bytes, key/column binding drift, and
// malformed writes advancing clocks. Both direct get and range reads validate.
func TestCorruptEnvelopeAndMaterializedColumnsFailClosed(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		scope := testScope()
		key := Key{Family: Message, Scope: scope, Parts: [][]byte{[]byte("message")}}
		good := envelope(t, Message, memory.MessageRecord{MessageID: "message", SourceID: "source"})
		mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), key, good) })
		rawExec(t, c, "UPDATE "+c.table("message")+" SET source_id=$1", []byte("foreign"))

		for _, scan := range []bool{false, true} {
			err := c.View(t.Context(), func(tx *Tx) error {
				if scan {
					_, err := tx.Scan(t.Context(), Range{Family: Message, Scope: &scope, Limit: 1})

					return err
				}

				_, err := tx.Get(t.Context(), key)

				return err
			})
			assertCode(t, err, memory.CodeInvalidDerived)
		}

		rawExec(t, c, "UPDATE "+c.table("message")+" SET envelope=$1", []byte("corrupt"))
		err := c.View(t.Context(), func(tx *Tx) error {
			_, err := tx.Get(t.Context(), key)

			return err
		})
		assertCode(t, err, memory.CodeInvalidDerived)

		wrongID := envelope(t, Message, memory.MessageRecord{MessageID: "foreign", SourceID: "source"})
		for _, bad := range [][]byte{[]byte("corrupt"), envelope(t, Source, true), wrongID} {
			err := c.Update(t.Context(), func(tx *Tx) error { return tx.Put(t.Context(), key, bad) })
			assertCode(t, err, memory.CodeInvalidDerived)
		}

		oversized := bytes.Repeat([]byte{1}, MaxEnvelopeBytes+1)
		err = c.Update(t.Context(), func(tx *Tx) error { return tx.Put(t.Context(), key, oversized) })
		assertCode(t, err, memory.CodeLimitExceeded)
		assertClock(t, c, 1, 1)
	})
}

// Catches raw SQL/server diagnostics escaping stable errors, or losing the
// context identity required by cancellation-aware callers.
func TestDatabaseErrorsAreSanitizedAndClassified(t *testing.T) {
	for _, tc := range []struct {
		state string
		code  memory.ErrorCode
		class memory.ErrorClass
	}{
		{"23505", memory.CodeConflict, memory.ErrorClassPermanent},
		{"40001", memory.CodeStoreFailure, memory.ErrorClassRetryable},
		{"40P01", memory.CodeStoreFailure, memory.ErrorClassRetryable},
		{"08006", memory.CodeStoreFailure, memory.ErrorClassRetryable},
		{"53100", memory.CodeStoreFailure, memory.ErrorClassRetryable},
		{"22000", memory.CodeStoreFailure, memory.ErrorClassPermanent},
	} {
		cause := &pgconn.PgError{Code: tc.state, Message: "password=secret", Detail: "private payload"}
		err := databaseError(t.Context(), "read", cause)

		code, class, _ := memory.ClassifyError(err)
		if code != tc.code || class != tc.class {
			t.Fatalf("classification for %s: %s/%s", tc.state, code, class)
		}

		if bytes.Contains([]byte(err.Error()), []byte("secret")) || bytes.Contains([]byte(err.Error()), []byte("payload")) {
			t.Fatal("diagnostics leaked")
		}
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := databaseError(ctx, "read", errors.New("secret")); !errors.Is(err, context.Canceled) {
		t.Fatal("context identity lost")
	}
}
