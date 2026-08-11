package memory

import "time"

// SourceRecord stores either an active raw turn or an identity-only tombstone.
type SourceRecord struct {
	SchemaVersion string      `json:"schema_version"`
	Ref           SourceRef   `json:"ref"`
	State         SourceState `json:"state"`
	Turn          *Turn       `json:"turn,omitempty"`
	ForgottenAt   *time.Time  `json:"forgotten_at,omitempty"`
}

// ScopeSnapshot is one versioned exact-scope persistence view.
type ScopeSnapshot struct {
	SchemaVersion string         `json:"schema_version"`
	Scope         Scope          `json:"scope"`
	Version       uint64         `json:"version"`
	Sources       []SourceRecord `json:"sources,omitempty"`
	Atoms         []Atom         `json:"atoms,omitempty"`
	Scenarios     []Scenario     `json:"scenarios,omitempty"`
	Profiles      []Profile      `json:"profiles,omitempty"`
}

// ScopeView is bounded same-scope derived context supplied to a model port.
type ScopeView struct {
	SchemaVersion string     `json:"schema_version"`
	Scope         Scope      `json:"scope"`
	Version       uint64     `json:"version"`
	Atoms         []Atom     `json:"atoms,omitempty"`
	Scenarios     []Scenario `json:"scenarios,omitempty"`
	Profiles      []Profile  `json:"profiles,omitempty"`
}
