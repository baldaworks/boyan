package memory

// TraceRequest asks for one bounded provenance graph in an exact scope.
type TraceRequest struct {
	SchemaVersion string      `json:"schema_version"`
	Scope         Scope       `json:"scope"`
	Root          RevisionRef `json:"root"`
	MaxNodes      int         `json:"max_nodes"`
}

// TraceGraph is the Store-owned full provenance subgraph for one root.
type TraceGraph struct {
	SchemaVersion string         `json:"schema_version"`
	Scope         Scope          `json:"scope"`
	Root          RevisionRef    `json:"root"`
	Revisions     []SearchHit    `json:"revisions"`
	Sources       []SourceRecord `json:"sources"`
}

// TraceResponse is a validated, closed provenance graph marked untrusted.
type TraceResponse struct {
	SchemaVersion string         `json:"schema_version"`
	Trust         ReferenceTrust `json:"trust"`
	Scope         Scope          `json:"scope"`
	Root          RevisionRef    `json:"root"`
	Revisions     []SearchHit    `json:"revisions"`
	Sources       []SourceRecord `json:"sources"`
}
