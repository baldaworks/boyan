package mcp

import memory "github.com/baldaworks/boyan/pkg/memory"

func publicErrorMessage(err error) string {
	return publicErrorMessageForCode(classifyErrorCode(err, memory.CodePermanent))
}

func publicErrorMessageForCode(code memory.ErrorCode) string {
	switch code {
	case memory.CodeInvalidQuery, memory.CodeInvalidDerived:
		return messageInvalidInput
	case memory.CodeDisabled:
		return messageDisabled
	case memory.CodeInvalidScope:
		return messageInvalidScope
	case memory.CodeUnsupportedScope:
		return messageUnsupported
	case memory.CodeUnavailable:
		return messageUnavailable
	case memory.CodeTimeout:
		return messageTimeout
	case memory.CodeScopeViolation:
		return messageScopeViolation
	case memory.CodeShuttingDown:
		return messageShuttingDown
	case memory.CodeConflict:
		return messageConflict
	case memory.CodeLimitExceeded:
		return messageBounded
	default:
		return messagePermanent
	}
}
