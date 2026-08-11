package memory

import (
	"math"
	"strings"
)

// Validate verifies a structured untrusted recall reference.
func (r DerivedReference) Validate() error {
	if r.SchemaVersion != DerivedSchemaVersionV1 {
		return invalidDerived("unsupported derived reference schema version")
	}
	if r.Trust != ReferenceTrustUntrusted {
		return invalidDerived("derived reference must be marked untrusted")
	}
	if err := r.Kind.Validate(); err != nil {
		return err
	}
	if err := r.Scope.Validate(); err != nil {
		return err
	}
	if err := (RevisionRef{ItemID: r.ItemID, RevisionID: r.RevisionID}).Validate(); err != nil {
		return err
	}
	if r.Revision == 0 || r.State != RevisionStateActive || r.CreatedAt.IsZero() {
		return invalidDerived("derived reference requires an active revision identity")
	}
	if strings.TrimSpace(r.Text) == "" || len(r.Text) > MaxDerivedTextBytes {
		return invalidDerived("derived reference text is invalid")
	}
	if err := r.Provenance.Validate(r.Scope); err != nil {
		return err
	}
	if r.Score != nil && (math.IsNaN(*r.Score) || math.IsInf(*r.Score, 0)) {
		return invalidDerived("derived reference score must be finite")
	}
	switch r.Kind {
	case DerivedKindAtom:
		if r.Category == nil {
			return invalidDerived("atom reference requires a category")
		}
		if err := r.Category.Validate(); err != nil {
			return err
		}
		if r.TopicKey != "" || r.Title != "" {
			return invalidDerived("atom reference contains scenario fields")
		}
	case DerivedKindScenario:
		if r.Category != nil || strings.TrimSpace(r.TopicKey) == "" || strings.TrimSpace(r.Title) == "" {
			return invalidDerived("scenario reference fields are invalid")
		}
	case DerivedKindProfile:
		if r.Category != nil || r.TopicKey != "" || r.Title != "" {
			return invalidDerived("profile reference contains unrelated fields")
		}
	}
	return nil
}
