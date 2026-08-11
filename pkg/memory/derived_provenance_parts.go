package memory

import (
	"cmp"
	"slices"
)

func canonicalProvenanceParts(provenance Provenance) []string {
	raw := slices.Clone(provenance.RawSources)
	slices.SortFunc(raw, func(left, right SourceRef) int {
		leftParts := []string{left.Scope.Key, string(left.Scope.Kind), left.ExportID, left.ConversationID, left.SourceTurnID}
		rightParts := []string{right.Scope.Key, string(right.Scope.Kind), right.ExportID, right.ConversationID, right.SourceTurnID}
		for index := range leftParts {
			if order := cmp.Compare(leftParts[index], rightParts[index]); order != 0 {
				return order
			}
		}
		return 0
	})

	parents := slices.Clone(provenance.ParentRevisions)
	slices.SortFunc(parents, func(left, right RevisionRef) int {
		if order := cmp.Compare(left.ItemID, right.ItemID); order != 0 {
			return order
		}
		return cmp.Compare(left.RevisionID, right.RevisionID)
	})

	parts := make([]string, 0, len(raw)*6+len(parents)*3)
	for _, source := range raw {
		parts = append(parts,
			"raw",
			source.Scope.Key,
			string(source.Scope.Kind),
			source.ExportID,
			source.ConversationID,
			source.SourceTurnID,
		)
	}
	for _, parent := range parents {
		parts = append(parts, "parent", parent.ItemID, parent.RevisionID)
	}
	return parts
}
