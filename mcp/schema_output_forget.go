package mcp

import (
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// ForgetOutput is the stable structured result for Boyan forget tools.
type ForgetOutput struct {
	ToolOutcome

	Scope               *memory.Scope        `json:"scope,omitempty"                jsonschema:"exact scope affected by the forget operation"` //nolint:lll // Schema tags must remain a single literal.
	OperationID         string               `json:"operation_id,omitempty"         jsonschema:"canonical operation ID"`
	Kind                memory.ForgetKind    `json:"kind,omitempty"                 jsonschema:"forget operation kind"`
	SourceCount         uint64               `json:"source_count,omitempty"         jsonschema:"exact source count"`
	RevisionCount       uint64               `json:"revision_count,omitempty"       jsonschema:"exact revision count"`
	ReferencesTruncated bool                 `json:"references_truncated,omitempty" jsonschema:"true when both reference arrays are omitted above the 2048-reference bound"` //nolint:lll // Schema tags must remain a single literal.
	ScopeVersion        uint64               `json:"scope_version,omitempty"        jsonschema:"scope version after the canonical denial"`                                   //nolint:lll // Schema tags must remain a single literal.
	ForgottenAt         *time.Time           `json:"forgotten_at,omitempty"         jsonschema:"forget timestamp"`
	Sources             []memory.SourceRef   `json:"sources,omitempty"              jsonschema:"forgotten sources"`
	Revisions           []memory.RevisionRef `json:"revisions,omitempty"            jsonschema:"affected revisions"`
	Notice              string               `json:"notice"                         jsonschema:"fixed handling notice for content-free forget outcomes"` //nolint:lll // Schema tags must remain a single literal.
}
