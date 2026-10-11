package memory

import (
	"strings"
	"time"
)

func buildReconciliationItem(
	p PolicyRegistry,
	scope Scope,
	candidate SemanticCandidate,
	subject string,
	predicate string,
	qualifiers []string,
) (MemoryItem, error) {
	item := MemoryItem{Scope: scope, Kind: candidate.Kind}
	if candidate.Kind == MemoryKindState {
		item.MemoryKey = MemoryKey(
			reconciliationID("key", p.Version, scope.Key, subject, predicate, strings.Join(qualifiers, "\x00")),
		)
		item.ItemID = reconciliationID("item", scope.Key, string(item.MemoryKey))

		return item, nil
	}

	eventAt := ""
	if candidate.Memory.Temporal.EventAt != nil {
		eventAt = candidate.Memory.Temporal.EventAt.UTC().Format(time.RFC3339Nano)
	}

	item.ItemID = reconciliationID(
		"event",
		scope.Key,
		candidate.Memory.Evidence[0].SourceID,
		candidate.Memory.Evidence[0].MessageID,
		eventAt,
		subject,
		predicate,
		strings.Join(qualifiers, "\x00"),
	)

	return item, nil
}
