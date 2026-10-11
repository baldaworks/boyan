package mcp

// ToolError is the stable error shape returned by Boyan MCP tools.
type ToolError struct {
	Operation string `json:"operation" jsonschema:"tool name that produced the error"`
	Code      string `json:"code"      jsonschema:"stable machine-readable error code"`
	Message   string `json:"message"   jsonschema:"safe human-readable error message"`
}

// ToolOutcome reports whether a Boyan MCP operation completed successfully.
type ToolOutcome struct {
	OK    bool       `json:"ok"              jsonschema:"true when the tool completed successfully"`
	Error *ToolError `json:"error,omitempty" jsonschema:"error details when ok is false"`
}
