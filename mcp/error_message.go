package mcp

import memory "github.com/baldaworks/boyan/pkg/memory"

func publicErrorMessage(err error) string {
	return publicErrorMessageForCode(classifyErrorCode(err, memory.CodePermanent))
}

var publicErrorMessages = map[memory.ErrorCode]string{
	memory.CodeInvalidQuery:     messageInvalidInput,
	memory.CodeInvalidDerived:   messageInvalidInput,
	memory.CodeDisabled:         messageDisabled,
	memory.CodeInvalidScope:     messageInvalidScope,
	memory.CodeUnsupportedScope: messageUnsupported,
	memory.CodeUnavailable:      messageUnavailable,
	memory.CodeTimeout:          messageTimeout,
	memory.CodeScopeViolation:   messageScopeViolation,
	memory.CodeShuttingDown:     messageShuttingDown,
	memory.CodeConflict:         messageConflict,
	memory.CodeLimitExceeded:    messageBounded,
}

func publicErrorMessageForCode(code memory.ErrorCode) string {
	if message, found := publicErrorMessages[code]; found {
		return message
	}

	return messagePermanent
}
