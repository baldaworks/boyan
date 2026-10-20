package canonicalsql

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"math"
	"testing"

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
