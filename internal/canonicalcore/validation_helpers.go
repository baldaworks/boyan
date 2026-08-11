package canonicalcore

import (
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

const maxCanonicalReadRecords = 256

func normalizeDerivedText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func newError(code memory.ErrorCode, class memory.ErrorClass, message string, cause error) error {
	return &memory.Error{Code: code, Class: class, Message: strings.TrimSpace(message), Cause: cause}
}

func invalidDerived(message string) error {
	return memory.PermanentError(memory.CodeInvalidDerived, message, nil)
}

func limitExceeded(message string) error {
	return memory.PermanentError(memory.CodeLimitExceeded, message, nil)
}

func isCanonicalID(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n\t")
}
