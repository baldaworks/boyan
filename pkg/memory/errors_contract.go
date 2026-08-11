package memory

// ErrorCode is a stable provider and adapter outcome code.
type ErrorCode string

const (
	// CodeDisabled means the requested Boyan capability is not enabled.
	CodeDisabled ErrorCode = "disabled"
	// CodeInvalidScope means a locator scope is malformed.
	CodeInvalidScope ErrorCode = "invalid_scope"
	// CodeUnsupportedScope means no safe classifier exists for a locator.
	CodeUnsupportedScope ErrorCode = "unsupported_scope"
	// CodeInvalidConversation means stable conversation identity is missing or malformed.
	CodeInvalidConversation ErrorCode = "invalid_conversation"
	// CodeInvalidQuery means a search query or limit is invalid.
	CodeInvalidQuery ErrorCode = "invalid_query"
	// CodeScopeViolation means a provider returned data for another scope.
	CodeScopeViolation ErrorCode = "scope_violation"
	// CodeUnavailable means a provider is temporarily unavailable.
	CodeUnavailable ErrorCode = "unavailable"
	// CodeTimeout means a bounded provider operation timed out.
	CodeTimeout ErrorCode = "timeout"
	// CodePermanent means a provider rejected an operation permanently.
	CodePermanent ErrorCode = "permanent"
	// CodeShuttingDown means the integration is no longer accepting work.
	CodeShuttingDown ErrorCode = "shutting_down"
	// CodeInvalidDerived means derived memory or provenance is malformed.
	CodeInvalidDerived ErrorCode = "invalid_derived"
	// CodeConflict means optimistic concurrency or revision state changed.
	CodeConflict ErrorCode = "conflict"
	// CodeNotFound means a requested source or revision does not exist.
	CodeNotFound ErrorCode = "not_found"
	// CodeForgotten means content was removed by a forgetting operation.
	CodeForgotten ErrorCode = "forgotten"
	// CodeLimitExceeded means a derived-memory hard bound was exceeded.
	CodeLimitExceeded ErrorCode = "limit_exceeded"
	// CodeStoreFailure means the persistence port failed.
	CodeStoreFailure ErrorCode = "store_failure"
	// CodeModelFailure means a model port failed.
	CodeModelFailure ErrorCode = "model_failure"
)

// ErrorClass controls whether a failed operation may be retried.
type ErrorClass string

const (
	// ErrorClassRetryable permits a bounded retry with the same idempotency key.
	ErrorClassRetryable ErrorClass = "retryable"
	// ErrorClassPermanent forbids automatic retry of the same invalid request.
	ErrorClassPermanent ErrorClass = "permanent"
)
