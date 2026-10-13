package memory

import (
	"strings"
	"time"
)

// CanonicalTraceRevision exposes native identity only within a trace graph.
// Derived search hits retain their separate, strict legacy identity contract.
type CanonicalTraceRevision struct {
	SchemaVersion string         `json:"schema_version"`
	Scope         Scope          `json:"scope"`
	ItemID        string         `json:"item_id"`
	RevisionID    string         `json:"revision_id"`
	Revision      uint64         `json:"revision"`
	Kind          MemoryKind     `json:"kind"`
	State         RevisionState  `json:"state"`
	Text          string         `json:"text"`
	CreatedAt     time.Time      `json:"created_at"`
	Temporal      Temporal       `json:"temporal"`
	Provenance    Provenance     `json:"provenance"`
	Sensitivity   Sensitivity    `json:"sensitivity"`
	Retention     RetentionClass `json:"retention"`
	Supersedes    *RevisionRef   `json:"supersedes,omitempty"`
}

func (r CanonicalTraceRevision) traceMeta() (RevisionMeta, error) {
	if err := r.validateContent(); err != nil {
		return RevisionMeta{}, err
	}

	if err := r.Provenance.Validate(r.Scope); err != nil {
		return RevisionMeta{}, err
	}

	if err := r.validateSupersedes(); err != nil {
		return RevisionMeta{}, err
	}

	return RevisionMeta{SchemaVersion: DerivedSchemaVersionV1,
			Scope:      r.Scope,
			ItemID:     r.ItemID,
			RevisionID: r.RevisionID,
			Revision:   r.Revision,
			State:      r.State,
			CreatedAt:  r.CreatedAt,
			Provenance: r.Provenance,
			Supersedes: r.Supersedes},
		nil
}

func (r CanonicalTraceRevision) validateContent() error {
	if err := r.validateIdentity(); err != nil {
		return err
	}

	if r.Kind != MemoryKindState && r.Kind != MemoryKindEvent {
		return invalidDerived("canonical trace kind is invalid")
	}

	if err := r.State.Validate(); err != nil {
		return err
	}

	if err := r.validateVisibility(); err != nil {
		return err
	}

	if err := r.validateText(); err != nil {
		return err
	}

	if err := r.Temporal.Validate(); err != nil {
		return err
	}

	return nil
}

func (r CanonicalTraceRevision) validateIdentity() error {
	if r.SchemaVersion != CanonicalSchemaVersionV1 ||
		!isCanonicalID(r.ItemID) ||
		!isCanonicalID(r.RevisionID) ||
		r.Revision == 0 {
		return invalidDerived("canonical trace identity is invalid")
	}

	if err := r.Scope.Validate(); err != nil {
		return err
	}

	return nil
}

func (r CanonicalTraceRevision) validateText() error {
	if strings.TrimSpace(r.Text) == "" ||
		len(r.Text) > MaxDerivedTextBytes ||
		r.CreatedAt.IsZero() ||
		!r.CreatedAt.Equal(r.Temporal.ObservedAt) {
		return invalidDerived("canonical trace content is invalid")
	}

	return nil
}

func (r CanonicalTraceRevision) validateSupersedes() error {
	if r.Supersedes == nil {
		return nil
	}

	if r.Revision == 1 {
		return invalidDerived("first canonical revision cannot supersede")
	}

	if err := r.Supersedes.Validate(); err != nil {
		return err
	}

	if r.Supersedes.ItemID != r.ItemID || r.Supersedes.RevisionID == r.RevisionID {
		return invalidDerived("canonical supersession identity is invalid")
	}

	for _, parent := range r.Provenance.ParentRevisions {
		if parent == *r.Supersedes {
			return nil
		}
	}

	return invalidDerived("canonical supersession parent is missing")
}

func (h SearchHit) traceMeta() (RevisionMeta, error) {
	if h.Canonical == nil {
		return h.Validate()
	}

	if h.Atom != nil || h.Scenario != nil || h.Profile != nil || h.Score != nil {
		return RevisionMeta{}, invalidDerived("trace must contain one revision without a score")
	}

	return h.Canonical.traceMeta()
}

func (r CanonicalTraceRevision) validateVisibility() error {
	if r.Sensitivity != SensitivityStandard {
		return PermanentError(CodeForgotten, "trace revision sensitivity is not allowed", nil)
	}

	if r.Retention != RetentionClassStandard && r.Retention != RetentionClassEphemeral {
		return PermanentError(CodeForgotten, "trace revision retention is not allowed", nil)
	}

	return nil
}
