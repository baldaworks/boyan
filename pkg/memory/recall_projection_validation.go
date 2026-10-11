package memory

import "strings"

// Validate verifies a projection document before it is indexed.
func (d RecallProjectionDocument) Validate() error {
	if err := d.Scope.Validate(); err != nil {
		return err
	}

	if !isCanonicalID(d.ItemID) || !isCanonicalID(d.RevisionID) || d.Revision == 0 ||
		strings.TrimSpace(d.Text) == "" ||
		len(d.Text) > MaxDerivedTextBytes ||
		d.CreatedAt.IsZero() {
		return invalidDerived("recall projection document identity or text is invalid")
	}

	switch d.Kind {
	case MemoryKindState, MemoryKindEvent:
	default:
		return invalidDerived("recall projection document kind is invalid")
	}

	return d.validateMetadata()
}

func (d RecallProjectionDocument) validateMetadata() error {
	if d.Category != nil {
		if err := d.Category.Validate(); err != nil {
			return err
		}
	}

	if err := d.Temporal.Validate(); err != nil {
		return err
	}

	if err := validateSensitivityRetention(d.Sensitivity, d.Retention); err != nil {
		return err
	}

	for _, sourceID := range d.SourceIDs {
		if !isCanonicalID(sourceID) {
			return invalidDerived("recall projection source identity is invalid")
		}
	}

	for _, conversationID := range d.ConversationIDs {
		if !isCanonicalID(conversationID) {
			return invalidDerived("recall projection conversation identity is invalid")
		}
	}

	if d.ScopeChangeSeq == 0 {
		return invalidDerived("recall projection change sequence is required")
	}

	return nil
}
