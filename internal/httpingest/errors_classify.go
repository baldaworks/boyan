package httpingest

import (
	stdhttp "net/http"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func statusFromError(err error) int {
	code, class, ok := memory.ClassifyError(err)
	if !ok {
		return stdhttp.StatusInternalServerError
	}
	switch code {
	case memory.CodeInvalidScope, memory.CodeUnsupportedScope, memory.CodeInvalidConversation, memory.CodeInvalidQuery, memory.CodeInvalidDerived:
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
