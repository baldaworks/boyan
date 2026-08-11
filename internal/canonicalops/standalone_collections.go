package canonicalops

import (
	"fmt"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func evidenceKey(ref memory.EvidenceRef) string {
	return strings.Join([]string{
		ref.SourceID,
		ref.MessageID,
		string(ref.Role),
		fmt.Sprintf("%d", ref.StartByte),
		fmt.Sprintf("%d", ref.EndByte),
		string(ref.AssertionMode),
		ref.TextDigest,
	}, ":")
}

func appendUniqueRefs(values []memory.RevisionRef, ref memory.RevisionRef) []memory.RevisionRef {
	for _, existing := range values {
		if existing == ref {
			return values
		}
	}
	return append(values, ref)
}

func appendUniqueStrings(values []string, next []string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	for _, value := range next {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}
