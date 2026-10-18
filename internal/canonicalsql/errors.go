package canonicalsql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net"
	"strings"
	"syscall"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func invalid(message string) error {
	return memory.PermanentError(memory.CodeInvalidDerived, message, nil)
}

func limitError() error {
	return memory.PermanentError(memory.CodeLimitExceeded, "canonical SQL bound exceeded", nil)
}

func conflict() error {
	return memory.PermanentError(memory.CodeConflict, "canonical SQL state changed", nil)
}

// Driver diagnostics can contain SQL arguments, payloads and connection strings.
// Classify before discarding them. Only context sentinels survive as causes.
func databaseError(ctx context.Context, operation string, err error) error {
	if err == nil {
		return nil
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}

	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
		return memory.PermanentError(memory.CodeNotFound, "canonical SQL row not found", nil)
	}

	return classifyDriverError(operation, err)
}

const (
	sqlitePrimaryCodeMask = 255
	sqliteBusy            = 5
	sqliteLocked          = 6
	sqliteConstraint      = 19
)

func classifyDriverError(operation string, err error) error {
	if errors.Is(err, sql.ErrConnDone) || errors.Is(err, driver.ErrBadConn) || errors.Is(err, pgconn.ErrConnClosed) {
		return memory.RetryableError(memory.CodeUnavailable, "canonical SQL connection unavailable", nil)
	}

	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return postgresError(operation, pg.Code)
	}

	if transientTransportError(err) {
		return memory.RetryableError(memory.CodeUnavailable, "canonical SQL connection unavailable", nil)
	}

	var sqlite interface{ Code() int }
	if errors.As(err, &sqlite) {
		switch sqlite.Code() & sqlitePrimaryCodeMask {
		case sqliteBusy, sqliteLocked:
			return memory.RetryableError(memory.CodeConflict, "canonical SQL busy", nil)
		case sqliteConstraint:
			return conflict()
		}
	}

	return memory.PermanentError(memory.CodeStoreFailure, "canonical SQL "+operation+" failed", nil)
}

// pgx wraps native dial/read/write errors. Classify only known transport
// failures, not every ConnectError or net.OpError: TLS, authentication and
// configuration failures must retain their permanent classification.
func transientTransportError(err error) bool {
	var op *net.OpError
	if !errors.As(err, &op) {
		return false
	}

	for _, cause := range []error{
		syscall.ECONNREFUSED, syscall.ECONNRESET, syscall.ECONNABORTED, syscall.EPIPE,
		syscall.ETIMEDOUT, syscall.ENETDOWN, syscall.ENETUNREACH, syscall.EHOSTUNREACH,
	} {
		if errors.Is(op.Err, cause) {
			return true
		}
	}

	return false
}

func postgresError(operation, code string) error {
	if code == "2200H" {
		return limitError()
	}

	if code == "23505" {
		return conflict()
	}

	for _, prefix := range []string{"08", "40", "53", "57"} {
		if strings.HasPrefix(code, prefix) {
			return memory.RetryableError(memory.CodeStoreFailure, "canonical SQL "+operation+" failed", nil)
		}
	}

	return memory.PermanentError(memory.CodeStoreFailure, "canonical SQL "+operation+" failed", nil)
}
