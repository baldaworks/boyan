package mcp

import (
	"time"
)

const (
	// SearchToolName is the standalone Boyan MCP search tool.
	SearchToolName = "boyan.search"
	// TraceToolName is the standalone Boyan MCP provenance tool.
	TraceToolName = "boyan.trace"
	// ForgetSourceToolName is the standalone Boyan MCP source forget tool.
	ForgetSourceToolName = "boyan.forget_source"
	// ForgetScopeToolName is the standalone Boyan MCP exact-scope forget tool.
	ForgetScopeToolName = "boyan.forget_scope"

	// DefaultTimeout bounds one Boyan MCP operation when no timeout is supplied.
	DefaultTimeout = 5 * time.Second
)

const (
	DataClassificationUntrustedReference = "untrusted_reference"
	DataClassificationNone               = "none"
	defaultServerName                    = "boyan"
	defaultServerTitle                   = "Boyan"
	defaultServerVersion                 = "0.0.0-dev"

	messageInvalidInput   = "boyan tool input is invalid"
	messageInvalidScope   = "scope is invalid"
	messageUnsupported    = "scope is unsupported"
	messageUnavailable    = "boyan memory service is unavailable"
	messageTimeout        = "boyan memory operation timed out"
	messageScopeViolation = "boyan memory service returned a foreign scope"
	messageDisabled       = "boyan memory service is disabled"
	messageShuttingDown   = "boyan memory service is shutting down"
	messageConflict       = "boyan memory request conflicted with durable state"
	messageBounded        = "boyan memory request exceeded a safety bound"
	messagePermanent      = "boyan memory operation failed"
)
