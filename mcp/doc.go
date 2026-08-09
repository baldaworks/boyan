// Package mcp exposes the standalone Boyan MCP surface.
//
// It registers boyan.search, boyan.trace, boyan.forget_source, and
// boyan.forget_scope, all scoped explicitly by exact scope_key and scope_kind.
// Search and trace results are bounded untrusted canonical references.
// Forget results are content-free durable acknowledgements.
package mcp
