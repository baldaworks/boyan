package mcp

import (
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// ScopeInput is the explicit exact-scope argument surface required by all
// standalone Boyan MCP tools.
type ScopeInput struct {
	ScopeKey  string           `json:"scope_key"  jsonschema:"exact opaque scope key"`
	ScopeKind memory.ScopeKind `json:"scope_kind" jsonschema:"exact opaque scope kind"`
}

// SearchInput is the complete public argument surface of boyan.search.
type SearchInput struct {
	ScopeInput

	Query             string               `json:"query"                          jsonschema:"text to search for in Boyan memory"`                                        //nolint:lll // Schema tags must remain a single literal.
	Limit             int                  `json:"limit,omitempty"                jsonschema:"maximum references to return; defaults to 10 and is bounded by the server"` //nolint:lll // Schema tags must remain a single literal.
	MemoryKind        *memory.MemoryKind   `json:"memory_kind,omitempty"          jsonschema:"canonical state or event kind filter"`                                      //nolint:lll // Schema tags must remain a single literal.
	Category          *memory.AtomCategory `json:"category,omitempty"             jsonschema:"atom category filter"`
	AsOf              *time.Time           `json:"as_of,omitempty"                jsonschema:"explicit validity timestamp"`
	SourceID          string               `json:"source_id,omitempty"            jsonschema:"exact source filter"`
	ConversationID    string               `json:"conversation_id,omitempty"      jsonschema:"exact conversation filter"`
	MemoryKey         string               `json:"memory_key,omitempty"           jsonschema:"canonical state memory key filter"`       //nolint:lll // Schema tags must remain a single literal.
	MinScopeChangeSeq uint64               `json:"min_scope_change_seq,omitempty" jsonschema:"minimum canonical scope change sequence"` //nolint:lll // Schema tags must remain a single literal.
}

// TraceInput is the complete public argument surface of boyan.trace.
type TraceInput struct {
	ScopeInput

	ItemID     string `json:"item_id"             jsonschema:"logical canonical memory item identifier"`
	RevisionID string `json:"revision_id"         jsonschema:"immutable canonical memory revision identifier"`
	MaxNodes   int    `json:"max_nodes,omitempty" jsonschema:"maximum provenance nodes; defaults to the server bound"`
}

// ForgetSourceInput is the complete public argument surface of boyan.forget_source.
type ForgetSourceInput struct {
	ScopeInput

	ExportID       string `json:"export_id"       jsonschema:"immutable source export identifier"`
	ConversationID string `json:"conversation_id" jsonschema:"exact conversation identifier for the source"`
	SourceTurnID   string `json:"source_turn_id"  jsonschema:"exact source turn identifier"`
}

// ForgetScopeInput is the complete public argument surface of boyan.forget_scope.
type ForgetScopeInput struct {
	ScopeInput

	RequestID       string `json:"request_id"        jsonschema:"caller-owned idempotency key"`
	ConfirmScopeKey string `json:"confirm_scope_key" jsonschema:"must exactly equal scope_key"`
}

type searchServiceInput = SearchInput
type traceServiceInput = TraceInput
type forgetSourceServiceInput = ForgetSourceInput
type forgetScopeServiceInput = ForgetScopeInput
