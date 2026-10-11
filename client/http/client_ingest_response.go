package clienthttp

import (
	"encoding/json"
	"fmt"
	"io"
	stdhttp "net/http"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func decodeResponse[T any](body io.Reader, dst *T) error {
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		return err
	}

	if err := decoder.Decode(new(struct{})); err != io.EOF {
		return fmt.Errorf("response contained trailing data")
	}

	return nil
}

func classifyStatus(status int, code memory.ErrorCode, class memory.ErrorClass) error {
	if code != "" && class != "" {
		switch class {
		case memory.ErrorClassRetryable:
			return memory.RetryableError(code, "http ingest request failed", nil)
		case memory.ErrorClassPermanent:
			return memory.PermanentError(code, "http ingest request failed", nil)
		}
	}

	switch status {
	case stdhttp.StatusConflict:
		return memory.RetryableError(memory.CodeConflict, "http ingest request failed", nil)
	case stdhttp.StatusTooManyRequests, stdhttp.StatusServiceUnavailable:
		return memory.RetryableError(memory.CodeUnavailable, "http ingest request failed", nil)
	case stdhttp.StatusGatewayTimeout:
		return memory.RetryableError(memory.CodeTimeout, "http ingest request failed", nil)
	case stdhttp.StatusBadRequest:
		return memory.PermanentError(memory.CodeInvalidDerived, "http ingest request failed", nil)
	default:
		return memory.PermanentError(memory.CodePermanent, "http ingest request failed", nil)
	}
}
