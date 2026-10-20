package canonicalsql

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestWrappedTransportClassification(t *testing.T) {
	cases := []struct {
		name      string
		cause     error
		retryable bool
	}{
		{"refused", syscall.ECONNREFUSED, true},
		{"reset", syscall.ECONNRESET, true},
		{"aborted", syscall.ECONNABORTED, true},
		{"broken_pipe", syscall.EPIPE, true},
		{"timeout", syscall.ETIMEDOUT, true},
		{"network_down", syscall.ENETDOWN, true},
		{"network_unreachable", syscall.ENETUNREACH, true},
		{"host_unreachable", syscall.EHOSTUNREACH, true},
		{"permission", syscall.EACCES, false},
		{"tls_identity", &tls.CertificateVerificationError{Err: x509.HostnameError{
			Certificate: &x509.Certificate{}, Host: "private-transport-test-secret",
		}}, false},
		{"tls_trust", &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}, false},
		{"unknown", errors.New("private-transport-test-secret"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cause := fmt.Errorf("private-transport-test-secret: %w", &net.OpError{
				Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", tc.cause),
			})
			err := databaseError(t.Context(), "connect PostgreSQL", cause)
			assertPrivateTransportError(t, err, tc.retryable, "private-transport-test-secret")
		})
	}
}

func assertPrivateTransportError(t *testing.T, err error, retryable bool, secrets ...string) {
	t.Helper()

	wantCode := memory.CodeStoreFailure
	if retryable {
		wantCode = memory.CodeUnavailable
	}

	code, _, ok := memory.ClassifyError(err)
	if !ok || code != wantCode || memory.IsRetryable(err) != retryable {
		t.Fatalf("transport classification=%s retryable=%v, want %s retryable=%v",
			code, memory.IsRetryable(err), wantCode, retryable)
	}

	if errors.Unwrap(err) != nil {
		t.Fatal("native error cause escaped the private boundary")
	}

	for _, secret := range secrets {
		if strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), secret) {
			t.Fatal("private diagnostic escaped transport boundary")
		}
	}
}

func TestTransportContextAndPermanentControls(t *testing.T) {
	reset := &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()

	expired, expire := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer expire()

	for _, ctx := range []context.Context{canceled, expired} {
		if err := databaseError(ctx, "connect PostgreSQL", reset); err != ctx.Err() {
			t.Fatal("caller context did not take precedence over transport classification")
		}
	}

	for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded} {
		cause := fmt.Errorf("private-context-diagnostic: %w", sentinel)
		if err := databaseError(t.Context(), "connect PostgreSQL", cause); err != sentinel {
			t.Fatal("wrapped context identity lost")
		}
	}

	for _, cause := range []error{
		&pgconn.PgError{Code: "28P01", Message: "private-transport-test-secret"},
		&pgconn.PgError{Code: "28000", Message: "private-transport-test-secret"},
		&pgconn.ParseConfigError{ConnString: "private-transport-test-secret"},
	} {
		err := databaseError(t.Context(), "connect PostgreSQL", cause)
		assertPrivateTransportError(t, err, false, "private-transport-test-secret")
	}
}
