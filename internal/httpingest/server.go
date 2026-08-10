package httpingest

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	stdhttp "net/http"
	"strings"
	"time"

	portableapp "github.com/baldaworks/boyan/internal/apps/boyan"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

const (
	defaultMaxBodyBytes   int64         = 1 << 20
	defaultRequestTimeout time.Duration = 15 * time.Second
)

// ReadyChecker reports whether the local HTTP service is currently prepared to accept work.
type ReadyChecker interface {
	CheckReadiness(ctx context.Context) error
}

// Config wires the strict versioned HTTP/JSON ingest handler.
type Config struct {
	Turns          portableapp.SequencedTurnIngestor
	Boundaries     portableapp.SequencedBoundaryIngestor
	Ready          ReadyChecker
	MaxBodyBytes   int64
	RequestTimeout time.Duration
}

// NewHandler constructs the strict versioned Boyan HTTP ingest surface.
func NewHandler(cfg Config) (stdhttp.Handler, error) {
	if cfg.Turns == nil || cfg.Boundaries == nil {
		return nil, memory.PermanentError(memory.CodeDisabled, "http ingest capabilities are required", nil)
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = defaultMaxBodyBytes
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = defaultRequestTimeout
	}
	mux := stdhttp.NewServeMux()
	mux.HandleFunc("/livez", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.Method != stdhttp.MethodGet {
			writeSafeError(w, r, stdhttp.StatusMethodNotAllowed, memory.PermanentError(memory.CodePermanent, "method not allowed", nil), nil)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(stdhttp.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("/readyz", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.Method != stdhttp.MethodGet {
			writeSafeError(w, r, stdhttp.StatusMethodNotAllowed, memory.PermanentError(memory.CodePermanent, "method not allowed", nil), nil)
			return
		}
		if cfg.Ready != nil {
			ctx, cancel := context.WithTimeout(r.Context(), cfg.RequestTimeout)
			defer cancel()
			if err := cfg.Ready.CheckReadiness(ctx); err != nil {
				writeSafeError(w, r, stdhttp.StatusServiceUnavailable, err, nil)
				return
			}
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(stdhttp.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("/v1/ingest/turns", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		serveJSON(w, r, cfg.RequestTimeout, cfg.MaxBodyBytes, func(ctx context.Context, body []byte) (memory.IngestReceipt, error) {
			var envelope memory.IngestEnvelope[memory.Turn]
			if err := decodeEnvelope(body, &envelope); err != nil {
				return memory.IngestReceipt{}, err
			}
			if err := envelope.Validate(func(turn memory.Turn) error { return turn.Validate() }); err != nil {
				return memory.IngestReceipt{}, err
			}
			fingerprint, err := memory.TurnIngestFingerprint(envelope)
			if err != nil {
				return memory.IngestReceipt{}, err
			}
			outcome, err := cfg.Turns.IngestSequencedTurn(ctx, memory.SequencedTurn{
				ProducerID:    envelope.ProducerID,
				ScopeSequence: envelope.ScopeSequence,
				Fingerprint:   fingerprint,
				Turn:          envelope.Payload,
			})
			if err != nil {
				return memory.IngestReceipt{}, err
			}
			return memory.IngestReceiptFromOutcome(outcome)
		})
	})
	mux.HandleFunc("/v1/ingest/boundaries", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		serveJSON(w, r, cfg.RequestTimeout, cfg.MaxBodyBytes, func(ctx context.Context, body []byte) (memory.IngestReceipt, error) {
			var envelope memory.IngestEnvelope[memory.Boundary]
			if err := decodeEnvelope(body, &envelope); err != nil {
				return memory.IngestReceipt{}, err
			}
			if err := envelope.Validate(func(boundary memory.Boundary) error { return boundary.Validate() }); err != nil {
				return memory.IngestReceipt{}, err
			}
			fingerprint, err := memory.BoundaryIngestFingerprint(envelope)
			if err != nil {
				return memory.IngestReceipt{}, err
			}
			outcome, err := cfg.Boundaries.ApplySequencedBoundary(ctx, memory.SequencedBoundary{
				ProducerID:    envelope.ProducerID,
				ScopeSequence: envelope.ScopeSequence,
				Fingerprint:   fingerprint,
				Boundary:      envelope.Payload,
			})
			if err != nil {
				return memory.IngestReceipt{}, err
			}
			return memory.IngestReceiptFromOutcome(outcome)
		})
	})
	return mux, nil
}

// ValidateLoopbackAddress rejects wildcard and non-loopback listener addresses.
func ValidateLoopbackAddress(addr string) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return memory.PermanentError(memory.CodeInvalidDerived, "listener address is required", nil)
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return memory.PermanentError(memory.CodeInvalidDerived, "listener address is invalid", err)
	}
	host = strings.Trim(host, "[]")
	switch strings.ToLower(host) {
	case "localhost":
		return nil
	case "":
		return memory.PermanentError(memory.CodeInvalidDerived, "listener address must bind loopback only", nil)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return memory.PermanentError(memory.CodeInvalidDerived, "listener address must bind loopback only", nil)
	}
	return nil
}

func serveJSON(w stdhttp.ResponseWriter, r *stdhttp.Request, timeout time.Duration, maxBodyBytes int64, handle func(context.Context, []byte) (memory.IngestReceipt, error)) {
	if r.Method != stdhttp.MethodPost {
		writeSafeError(w, r, stdhttp.StatusMethodNotAllowed, memory.PermanentError(memory.CodePermanent, "method not allowed", nil), nil)
		return
	}
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		writeSafeError(w, r, stdhttp.StatusBadRequest, memory.PermanentError(memory.CodeInvalidDerived, "content type must be application/json", nil), nil)
		return
	}
	limited := stdhttp.MaxBytesReader(w, r.Body, maxBodyBytes)
	defer limited.Close()
	body, err := io.ReadAll(limited)
	if err != nil {
		writeSafeError(w, r, statusFromError(memory.PermanentError(memory.CodeLimitExceeded, "request body exceeds limit", err)), memory.PermanentError(memory.CodeLimitExceeded, "request body exceeds limit", err), nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	receipt, err := handle(ctx, body)
	if err != nil {
		writeSafeError(w, r, statusFromError(err), err, nil)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(stdhttp.StatusOK)
	_ = json.NewEncoder(w).Encode(receipt)
}

func decodeEnvelope[T any](body []byte, dst *T) error {
	if len(body) == 0 {
		return memory.PermanentError(memory.CodeInvalidDerived, "request body is required", nil)
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return memory.PermanentError(memory.CodeInvalidDerived, "request body is invalid", err)
	}
	if err := decoder.Decode(new(struct{})); !errors.Is(err, io.EOF) {
		return memory.PermanentError(memory.CodeInvalidDerived, "request body must contain exactly one JSON object", err)
	}
	return nil
}

func isJSONContentType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	return err == nil && mediaType == "application/json"
}

func writeSafeError(w stdhttp.ResponseWriter, _ *stdhttp.Request, status int, err error, retryAfter *time.Duration) {
	code, class, ok := memory.ClassifyError(err)
	if !ok {
		code = memory.CodePermanent
		class = memory.ErrorClassPermanent
	}
	body := memory.IngestErrorResponse{
		Code:    code,
		Class:   class,
		Message: safeErrorMessage(code, status),
	}
	if retryAfter != nil {
		delay := retryAfter.Milliseconds()
		body.RetryAfterMS = &delay
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func statusFromError(err error) int {
	code, class, ok := memory.ClassifyError(err)
	if !ok {
		return stdhttp.StatusInternalServerError
	}
	switch code {
	case memory.CodeInvalidScope, memory.CodeUnsupportedScope, memory.CodeInvalidSession, memory.CodeInvalidQuery, memory.CodeInvalidDerived:
		return stdhttp.StatusBadRequest
	case memory.CodeConflict:
		return stdhttp.StatusConflict
	case memory.CodeLimitExceeded:
		if class == memory.ErrorClassRetryable {
			return stdhttp.StatusTooManyRequests
		}
		return stdhttp.StatusBadRequest
	case memory.CodeTimeout:
		return stdhttp.StatusGatewayTimeout
	case memory.CodeUnavailable, memory.CodePermanent, memory.CodeShuttingDown, memory.CodeStoreFailure, memory.CodeModelFailure, memory.CodeDisabled:
		return stdhttp.StatusServiceUnavailable
	default:
		return stdhttp.StatusInternalServerError
	}
}

func safeErrorMessage(code memory.ErrorCode, status int) string {
	switch status {
	case stdhttp.StatusBadRequest:
		return "ingest request is invalid"
	case stdhttp.StatusConflict:
		return "ingest request conflicted with durable state"
	case stdhttp.StatusTooManyRequests:
		return "ingest service is temporarily at capacity"
	case stdhttp.StatusServiceUnavailable:
		if code == memory.CodeShuttingDown {
			return "ingest service is shutting down"
		}
		return "ingest service is temporarily unavailable"
	case stdhttp.StatusGatewayTimeout:
		return "ingest request timed out"
	default:
		return "ingest request failed"
	}
}
