package mcp

import (
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// ForgetOutput is the stable structured result for Boyan forget tools.
type ForgetOutput struct {
	ToolOutcome

	Scope        *memory.Scope        `json:"scope,omitempty"         jsonschema:"exact scope affected by the forget operation"` //nolint:lll // Schema tags must remain a single literal.
	OperationID  string               `json:"operation_id,omitempty"  jsonschema:"durable canonical operation identifier"`
	Kind         memory.ForgetKind    `json:"kind,omitempty"          jsonschema:"forget operation kind"`
	ScopeVersion uint64               `json:"scope_version,omitempty" jsonschema:"scope version after the canonical denial"` //nolint:lll // Schema tags must remain a single literal.
	ForgottenAt  *time.Time           `json:"forgotten_at,omitempty"  jsonschema:"server-assigned forget timestamp"`
	Sources      []memory.SourceRef   `json:"sources,omitempty"       jsonschema:"forgotten source identities"`
	Revisions    []memory.RevisionRef `json:"revisions,omitempty"     jsonschema:"affected revision identities"`
	Notice       string               `json:"notice"                  jsonschema:"fixed handling notice for content-free forget outcomes"` //nolint:lll // Schema tags must remain a single literal.
}
